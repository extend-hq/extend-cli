package grade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
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
	BaseURL  string        // optional override, defaults to "https://api.anthropic.com"
	Timeout  time.Duration // per-call timeout; defaults to 60s
	Client   *http.Client  // optional override (tests inject a stub)
}

// JudgeFromEnv builds a JudgeConfig from environment defaults. The
// runner calls this and then optionally overrides via flags.
func JudgeFromEnv() JudgeConfig {
	return JudgeConfig{
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   "claude-opus-4-5", // judge default; consistent rubric across both harness outputs
		BaseURL: "https://api.anthropic.com",
		Timeout: 60 * time.Second,
	}
}

// checkJudge dispatches a judge expectation to the Anthropic Messages
// API and returns the verdict. Any infrastructure failure (no API key,
// network error, malformed response) returns Passed=false with an
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

// JudgeVerdict is the structured response shape the model is asked to
// emit. Mirrors skill-creator's three-field record but flattened for
// JSON parsing.
type JudgeVerdict struct {
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

// buildJudgePrompt assembles the judge prompt. Modelled on
// skill-creator/agents/grader.md: the judge sees the user's prompt,
// the eval's expected behaviour, the criterion to verify, the agent's
// final message, and a compact summary of every `extend` call the
// agent made. The judge is asked to emit a single JSON object.
//
// Discipline rules:
//   - Evidence MUST be a verbatim quote from the agent's response,
//     not a summary.
//   - The judge must apply the discriminating-assertion test: if the
//     verdict would also pass for a clearly wrong response, return
//     Passed=false.
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

	b.WriteString("OUTPUT FORMAT — reply with a single JSON object on its own line:\n")
	b.WriteString(`{"passed": true|false, "evidence": "<short verbatim quote or fact>"}` + "\n\n")
	b.WriteString("RULES:\n")
	b.WriteString("- `evidence` MUST be a verbatim quote from the agent's final message, OR a literal `extend ...` argv from the invocations list. Never paraphrase.\n")
	b.WriteString("- Apply the discriminating-assertion test: if your `passed` verdict would also pass for a clearly wrong agent response, you're being too lenient — return `passed: false`.\n")
	b.WriteString("- If the criterion is unverifiable from the available evidence, return `passed: false` with evidence explaining what's missing.\n")
	b.WriteString("- Output only the JSON object. No prose, no markdown fence.\n")

	return b.String()
}

// callJudge POSTs the prompt to the Anthropic Messages API and parses
// the assistant response as a JudgeVerdict. The API surface is held
// stable enough by anthropic-version=2023-06-01 that this single-shot
// client doesn't need a vendor SDK.
func callJudge(cfg JudgeConfig, prompt string) (*JudgeVerdict, error) {
	model := cfg.Model
	if model == "" {
		model = "claude-opus-4-5"
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
		client = &http.Client{Timeout: timeout}
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": 512,
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
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, snippetN(string(respBody), 200))
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

	verdict, err := parseVerdict(text)
	if err != nil {
		return nil, fmt.Errorf("parse verdict %q: %w", snippetN(text, 200), err)
	}
	return verdict, nil
}

// parseVerdict tolerates a few model-side quirks: leading/trailing
// prose, a code-fenced JSON object, or a bare JSON object.
func parseVerdict(s string) (*JudgeVerdict, error) {
	s = strings.TrimSpace(s)
	// Strip code fences if present.
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// If extra prose surrounds the JSON, isolate the first {...} run.
	if !strings.HasPrefix(s, "{") {
		re := regexp.MustCompile(`(?s)\{[^{}]*"passed"[^{}]*\}`)
		if m := re.FindString(s); m != "" {
			s = m
		}
	}
	var v JudgeVerdict
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func snippetN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
