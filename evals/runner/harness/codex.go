package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Codex is the OpenAI Codex CLI driver. It invokes `codex exec` with
// JSON output and isolation flags (--ephemeral, --skip-git-repo-check,
// --ignore-user-config, --ignore-rules).
type Codex struct {
	Bin string // path to `codex` binary; if empty, looked up on PATH
}

func (c *Codex) Name() string { return "codex" }

func (c *Codex) Available() error {
	bin := c.Bin
	if bin == "" {
		bin = "codex"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("codex binary not on PATH: %w", err)
	}
	return nil
}

func (c *Codex) Run(ctx context.Context, opts RunOptions) (*Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "codex"
	}

	if opts.SkillEnabled {
		if err := installSkillForCodex(opts.HomeDir); err != nil {
			return nil, fmt.Errorf("install skill: %w", err)
		}
	}

	args := []string{
		"exec",
		"--json",
		"--ephemeral",
		"--skip-git-repo-check",
		"--ignore-user-config",
		"--ignore-rules",
		"--dangerously-bypass-approvals-and-sandbox",
		"--cd", opts.ScratchDir,
	}
	if opts.Tune.Effort != "" {
		// Codex calls this `model_reasoning_effort`; values: minimal,
		// low, medium, high, xhigh. "minimal" disables image_gen and
		// web_search tools — we don't need those, but stick to "low"
		// for safety unless explicitly set.
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", opts.Tune.Effort))
	}
	if opts.Tune.FastMode {
		// Maps to OpenAI service_tier="fast"; 1.5x faster, 2.5x credit
		// rate. Requires ChatGPT login (not API key).
		args = append(args, "-c", `service_tier="fast"`)
	}
	args = append(args, opts.Prompt)

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Dir = opts.ScratchDir
	cmd.Env = baseEnv(opts)
	// Codex resolves auth from CODEX_HOME (defaults to $HOME/.codex).
	// Since we override HOME for skill isolation, we must explicitly
	// point CODEX_HOME at the host's real auth dir or every codex run
	// fails 401. Skill discovery is still isolated via HOME (codex
	// reads `$HOME/.agents/skills/extend/SKILL.md`).
	cmd.Env = append(cmd.Env, "CODEX_HOME="+hostCodexHome())

	stream, err := openStream(opts.EventsPath)
	if err != nil {
		return nil, err
	}
	defer stream.close()

	cmd.Stdout = stream.f
	stderr := strings.Builder{}
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	durMS := time.Since(start).Milliseconds()

	res := &Result{
		HarnessName: c.Name(),
		DurationMS:  durMS,
		Tokens:      -1,
	}
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			res.Aborted = true
		}
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}

	if events, perr := scanJSONLines(opts.EventsPath); perr == nil {
		res.FinalMessage = codexFinalMessage(events)
		res.Tokens = codexTokens(events)
		res.SkillRead = anyCodexSkillUsage(events)
		if opts.FinalMessagePath != "" && res.FinalMessage != "" {
			_ = os.WriteFile(opts.FinalMessagePath, []byte(res.FinalMessage), 0o644)
		}
	}

	return res, nil
}

// hostCodexHome returns the host's CODEX_HOME (or ~/.codex), used
// to share auth with the user's codex install when our isolated HOME
// would otherwise hide it.
func hostCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// installSkillForCodex generates the SKILL.md and writes it to the
// per-harness path under HomeDir. Codex reads from
// $HOME/.agents/skills/<name>/SKILL.md (the agentskills.io standard
// path; see https://developers.openai.com/codex/skills/).
func installSkillForCodex(homeDir string) error {
	dst := filepath.Join(homeDir, ".agents", "skills", "extend", "SKILL.md")
	return generateSkillTo(dst)
}

// codexFinalMessage walks the JSONL for the final assistant message.
// Codex emits `item.completed` events with `item.type=="agent_message"`
// containing the agent's text. (Codex CLI uses `type` for both the
// envelope event and the inner item discriminator; prior versions
// used `item_type`. We accept either.)
func codexFinalMessage(events []map[string]any) string {
	var last string
	for _, ev := range events {
		t, _ := ev["type"].(string)
		if t != "item.completed" {
			continue
		}
		item, ok := ev["item"].(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := item["type"].(string)
		if itemType == "" {
			itemType, _ = item["item_type"].(string)
		}
		if itemType != "agent_message" {
			continue
		}
		if txt, _ := item["text"].(string); txt != "" {
			last = txt
		}
	}
	return last
}

// codexTokens reads the final `turn.completed` event for usage.
func codexTokens(events []map[string]any) int {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if t, _ := ev["type"].(string); t != "turn.completed" {
			continue
		}
		usage, ok := ev["usage"].(map[string]any)
		if !ok {
			continue
		}
		input := intFrom(usage["input_tokens"])
		output := intFrom(usage["output_tokens"])
		return input + output
	}
	return -1
}

// anyCodexSkillUsage looks for evidence of skill activation: a
// command_execution item whose command reads SKILL.md, or a
// skill-loading event in the stream.
func anyCodexSkillUsage(events []map[string]any) bool {
	for _, ev := range events {
		t, _ := ev["type"].(string)
		switch t {
		case "item.completed", "item.started":
			item, ok := ev["item"].(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := item["command"].(string)
			if strings.Contains(cmd, "skills/extend/SKILL.md") {
				return true
			}
			// Codex may also emit a tool item type for skill loads;
			// treat any text mentioning the skill path as activation.
			text, _ := item["text"].(string)
			if strings.Contains(text, "skills/extend/SKILL.md") {
				return true
			}
		case "skill.loaded", "skill.activated":
			// If/when Codex surfaces explicit skill events.
			return true
		}
	}
	return false
}
