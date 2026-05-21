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

// Claude is the Claude Code driver. It invokes `claude -p` with
// stream-json output and isolation flags (`--no-session-persistence`,
// `--setting-sources project,local` to skip the user's actual config,
// HOME pointed at our scratch home).
type Claude struct {
	Bin string // path to `claude` binary; if empty, looked up on PATH
}

func (c *Claude) Name() string { return "claude_code" }

// Available returns nil if the harness is invocable.
func (c *Claude) Available() error {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("claude binary not on PATH: %w", err)
	}
	return nil
}

func (c *Claude) Run(ctx context.Context, opts RunOptions) (*Result, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}

	if opts.SkillEnabled {
		if err := installSkillForClaude(opts.HomeDir); err != nil {
			return nil, fmt.Errorf("install skill: %w", err)
		}
	}

	args := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		// stream-json requires --verbose when used with --print.
		"--verbose",
		"--no-session-persistence",
		// `bypassPermissions` auto-approves tool use so the agent can
		// invoke our stub `extend` without a permission dialog.
		"--permission-mode", "bypassPermissions",
	}
	if !opts.SkillEnabled {
		// Per the Claude Code CLI: --disable-slash-commands "Disable all
		// skills". Used for the without-skill baseline.
		args = append(args, "--disable-slash-commands")
	}
	if opts.Tune.Effort != "" {
		args = append(args, "--effort", opts.Tune.Effort)
	}
	// Anthropic priority tier is gated on a sales contract; no CLI flag
	// surfaces it. opts.Tune.FastMode is a no-op for claude.

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Dir = opts.ScratchDir
	cmd.Env = baseEnv(opts)

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
		// Distinguish timeout/cancel from a non-zero exit.
		if cctx.Err() == context.DeadlineExceeded {
			res.Aborted = true
		}
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}

	// Best-effort post-hoc analysis. Failures here are non-fatal —
	// graders run on partial data.
	if events, perr := scanJSONLines(opts.EventsPath); perr == nil {
		res.FinalMessage = claudeFinalMessage(events)
		res.Tokens = claudeTokens(events)
		res.SkillRead = anyClaudeSkillUsage(events)
		if opts.FinalMessagePath != "" && res.FinalMessage != "" {
			_ = os.WriteFile(opts.FinalMessagePath, []byte(res.FinalMessage), 0o644)
		}
	}

	return res, nil
}

// installSkillForClaude lays down SKILL.md and the references/ tree at
// $HOME/.claude/skills/extend/, the Claude Code-specific path.
func installSkillForClaude(homeDir string) error {
	return installSkillTo(filepath.Join(homeDir, ".claude", "skills", "extend"))
}

// claudeFinalMessage walks the parsed event stream for the agent's
// last completed assistant message text. Stream-json emits assistant
// messages with `type=assistant` and a `message.content` array; the
// last assistant message before the `result` event is the answer.
func claudeFinalMessage(events []map[string]any) string {
	var last string
	for _, ev := range events {
		t, _ := ev["type"].(string)
		if t != "assistant" {
			continue
		}
		msg, ok := ev["message"].(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, blk := range content {
			b, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := b["type"].(string); t != "text" {
				continue
			}
			if txt, _ := b["text"].(string); txt != "" {
				last = txt
			}
		}
	}
	return last
}

// claudeTokens reads the final `result` event for total token usage.
func claudeTokens(events []map[string]any) int {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if t, _ := ev["type"].(string); t != "result" {
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

// anyClaudeSkillUsage looks for evidence that the skill was activated:
// either a Skill tool-use event, or a Read tool-use targeting a path
// that contains "skills/extend/SKILL.md".
func anyClaudeSkillUsage(events []map[string]any) bool {
	for _, ev := range events {
		t, _ := ev["type"].(string)
		if t != "assistant" {
			continue
		}
		msg, ok := ev["message"].(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, blk := range content {
			b, ok := blk.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := b["type"].(string); t != "tool_use" {
				continue
			}
			name, _ := b["name"].(string)
			if name == "Skill" {
				return true
			}
			if name == "Read" {
				input, _ := b["input"].(map[string]any)
				p, _ := input["file_path"].(string)
				if strings.Contains(p, "skills/extend/SKILL.md") {
					return true
				}
			}
		}
	}
	return false
}

func intFrom(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	default:
		return 0
	}
}
