package grade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// JudgeConfig configures the LLM-judge. The runner constructs one
// instance per orchestrator invocation and threads it into Inputs.
//
// When Disabled is true (e.g. operator passed -no-judge for a cheap
// pre-PR sweep), all judge expectations remain Skipped — same shape
// as Phase 1/2 had before the judge wired in.
type JudgeConfig struct {
	Disabled bool
	APIKey   string        // ANTHROPIC_API_KEY; required when Disabled is false
	Model    string        // model ID, e.g. "claude-opus-4-7"
	Effort   string        // "low" | "medium" | "high" | "xhigh" | "max"; defaults to "low" for judging
	BaseURL  string        // optional override, defaults to "https://api.anthropic.com"
	Timeout  time.Duration // per-call timeout; defaults to 60s
	Client   *http.Client  // optional override (tests inject a stub)
}

// JudgeFromEnv builds a JudgeConfig from environment defaults. The
// runner calls this and then optionally overrides via flags.
//
// Defaults:
//   - Model: claude-opus-4-7 (consistent rubric across both harness outputs).
//   - Effort: low. Judging "did the agent's prose match this criterion?"
//     is a simple classification, not deep reasoning — Opus 4.7 at low
//     effort scopes its work to what was asked and runs much faster.
func JudgeFromEnv() JudgeConfig {
	return JudgeConfig{
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   "claude-opus-4-7",
		Effort:  "low",
		BaseURL: "https://api.anthropic.com",
		Timeout: 60 * time.Second,
	}
}

// checkJudge dispatches a judge expectation to the Anthropic Messages
// API and returns the verdict. Any infrastructure failure (no API key,
// network error, schema mismatch) returns Passed=false with an
// explanatory evidence string — so a flaky judge never silently passes
// a case.
func checkJudge(exp spec.Expectation, in Inputs, cfg JudgeConfig) (passed bool, skipped bool, evidence string) {
	if cfg.Disabled {
		return false, true, "judge disabled (-no-judge)"
	}
	if cfg.APIKey == "" {
		return false, true, "judge unavailable: ANTHROPIC_API_KEY not set"
	}
	if in.Harness == nil || in.Harness.FinalMessage == "" {
		return false, false, "no final agent message captured"
	}

	prompt := buildJudgePrompt(exp, in)
	verdict, err := callJudge(cfg, prompt)
	if err != nil {
		return false, false, "judge call failed: " + err.Error()
	}
	return verdict.Passed, false, verdict.Evidence
}

// JudgeVerdict is the structured response shape the model emits, bound
// by output_config.format on the API call. Structured outputs guarantee
// the response is valid JSON matching this shape — no parsing fallback
// needed.
type JudgeVerdict struct {
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

// judgeOutputSchema is the JSON Schema we hand to the API. The schema's
// description fields are visible to the model and reinforce the
// verbatim-quote rule from the prompt.
var judgeOutputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"passed": map[string]any{
			"type":        "boolean",
			"description": "true if the agent's response satisfies the criterion; false otherwise.",
		},
		"evidence": map[string]any{
			"type":        "string",
			"description": "A short verbatim quote from the agent's final message OR a literal `extend ...` argv from the invocations list. Never a paraphrase.",
		},
	},
	"required":             []string{"passed", "evidence"},
	"additionalProperties": false,
}

// buildJudgePrompt assembles the judge prompt. Modelled on
// skill-creator/agents/grader.md: the judge sees the user's prompt,
// the eval's expected behaviour, the criterion to verify, the agent's
// final message, and a compact summary of every `extend` call the
// agent made. Structured outputs guarantee the response shape; the
// prompt focuses on the discriminating-assertion rule and the
// verbatim-quote constraint.
func buildJudgePrompt(exp spec.Expectation, in Inputs) string {
	var b strings.Builder
	b.WriteString("You are a strict grader for an AI coding agent's response to a CLI-related prompt.\n\n")
	b.WriteString("USER PROMPT:\n\"\"\"\n")
	b.WriteString(in.Eval.Prompt)
	b.WriteString("\n\"\"\"\n\n")
	if in.Eval.ExpectedOutput != "" {
		b.WriteString("EXPECTED BEHAVIOUR (for context, not a test):\n\"\"\"\n")
		b.WriteString(in.Eval.ExpectedOutput)
		b.WriteString("\n\"\"\"\n\n")
	}
	b.WriteString("THE SPECIFIC CRITERION YOU MUST VERIFY:\n\"\"\"\n")
	b.WriteString(exp.Criterion)
	b.WriteString("\n\"\"\"\n\n")

	b.WriteString("AGENT'S FINAL MESSAGE:\n\"\"\"\n")
	b.WriteString(in.Harness.FinalMessage)
	b.WriteString("\n\"\"\"\n\n")

	if len(in.Calls) > 0 {
		b.WriteString("EXTEND CLI INVOCATIONS THE AGENT MADE (in order):\n")
		for i, c := range in.Calls {
			fmt.Fprintf(&b, "  %d. extend %s (exit=%d)\n", i+1, strings.Join(c.Argv, " "), c.ExitCode)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("(The agent made no `extend` invocations.)\n\n")
	}

	b.WriteString("RULES:\n")
	b.WriteString("- `evidence` MUST be a verbatim quote from the agent's final message, OR a literal `extend ...` argv from the invocations list. Never paraphrase.\n")
	b.WriteString("- Apply the discriminating-assertion test: if your `passed` verdict would also pass for a clearly wrong agent response, you're being too lenient — return `passed: false`.\n")
	b.WriteString("- If the criterion is unverifiable from the available evidence, return `passed: false` with evidence explaining what's missing.\n")

	return b.String()
}

// callJudge POSTs the prompt to the Anthropic Messages API and parses
// the assistant response as a JudgeVerdict. Uses structured outputs
// (output_config.format) so the response is guaranteed to be valid
// JSON matching judgeOutputSchema — no regex / fallback parsing.
func callJudge(cfg JudgeConfig, prompt string) (*JudgeVerdict, error) {
	model := cfg.Model
	if model == "" {
		model = "claude-opus-4-7"
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	client := cfg.Client
	if client == nil {
		// x-api-key is not one of the headers Go's client strips on a
		// cross-host redirect (only Authorization/Cookie are), so a
		// redirected Messages call would replay the Anthropic key to
		// whatever host the Location header names. Pin every hop to
		// the configured judge API origin.
		base, baseErr := url.Parse(baseURL)
		client = &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if baseErr != nil {
					return fmt.Errorf("refusing redirect: parse judge base url %q: %w", baseURL, baseErr)
				}
				if req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
					return fmt.Errorf("refusing redirect to %s://%s: not the judge API origin %s://%s",
						req.URL.Scheme, req.URL.Host, base.Scheme, base.Host)
				}
				return nil
			},
		}
	}

	outputConfig := map[string]any{
		"format": map[string]any{
			"type":   "json_schema",
			"schema": judgeOutputSchema,
		},
	}
	if cfg.Effort != "" {
		outputConfig["effort"] = cfg.Effort
	}

	body := map[string]any{
		"model":         model,
		"max_tokens":    1024,
		"output_config": outputConfig,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, snippetN(string(respBody), 240))
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}

	text := ""
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty assistant response")
	}

	// Structured outputs guarantee text is JSON matching judgeOutputSchema —
	// no parser fallback needed. Surface decode errors loudly if the
	// guarantee ever breaks.
	var v JudgeVerdict
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return nil, fmt.Errorf("decode structured verdict %q: %w", snippetN(text, 240), err)
	}
	return &v, nil
}

func snippetN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
