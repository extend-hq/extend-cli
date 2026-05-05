// Package harness drives Claude Code and Codex as subprocesses for one
// eval run. Each driver returns a normalized Result regardless of the
// underlying harness's JSONL shape.
package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunOptions carries everything one driver invocation needs.
type RunOptions struct {
	// Prompt is the user-style instruction handed to the agent verbatim.
	Prompt string

	// ScratchDir is the agent's working directory. Fixtures land here.
	ScratchDir string

	// HomeDir is the harness's isolated $HOME. With-skill installs land
	// here at the harness-specific path; without-skill leaves it bare.
	HomeDir string

	// StubDir is prepended to PATH so the agent's `extend` invocations
	// hit our recording fake.
	StubDir string

	// SkillEnabled controls whether SKILL.md is installed in HomeDir.
	SkillEnabled bool

	// Mode is "trigger" or "outcome". Phase 1: same runtime; Phase 2
	// adds early termination for trigger.
	Mode string

	// EventsPath is where to stream the harness's raw JSONL output.
	EventsPath string

	// FinalMessagePath is where to write the agent's last message.
	FinalMessagePath string

	// RecordPath is the EXTEND_EVAL_RECORD value passed to the stub.
	RecordPath string

	// Timeout caps total runtime. After Timeout elapses the harness is
	// killed; the run is marked Aborted.
	Timeout time.Duration

	// StubMode is the EXTEND_EVAL_MODE for this case. One of
	// real_responses, paginated, auth_error.
	StubMode string

	// ExtraEnv is appended to the harness's environment.
	ExtraEnv []string
}

// Result is the normalized output of one harness invocation.
type Result struct {
	// FinalMessage is the agent's last assistant message text. Empty
	// if the run aborted before the model produced one.
	FinalMessage string

	// Tokens is the total tokens consumed (input + output) per the
	// harness's usage event. -1 if unavailable.
	Tokens int

	// DurationMS is wall-clock duration as observed by the runner.
	DurationMS int64

	// ExitCode of the harness process. 0 on clean exit.
	ExitCode int

	// SkillRead is true if the run-time evidence shows the agent
	// touched the skill (read SKILL.md, or invoked the extend stub).
	SkillRead bool

	// Aborted is true if we killed the process due to timeout or
	// trigger-mode early termination.
	Aborted bool

	// HarnessName is "claude_code" or "codex".
	HarnessName string
}

// Driver is the harness contract.
type Driver interface {
	Name() string
	Available() error
	Run(ctx context.Context, opts RunOptions) (*Result, error)
}

// streamWriter copies the subprocess stdout into the events file
// line-by-line, returning the parsed lines so the caller can mine
// them post-hoc for the final message and token usage.
type streamWriter struct {
	path string
	bw   *bufio.Writer
	f    *os.File
}

func openStream(path string) (*streamWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &streamWriter{path: path, f: f, bw: bufio.NewWriter(f)}, nil
}

func (s *streamWriter) write(line []byte) error {
	if _, err := s.bw.Write(line); err != nil {
		return err
	}
	if !strings.HasSuffix(string(line), "\n") {
		_, _ = s.bw.WriteString("\n")
	}
	return nil
}

func (s *streamWriter) close() error {
	if s.bw != nil {
		_ = s.bw.Flush()
	}
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}

// scanJSONLines reads a JSONL file and yields each line as a parsed
// generic event for post-hoc analysis. Malformed lines are skipped
// silently — better to grade on partial data than fail the whole run.
func scanJSONLines(path string) ([]map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []map[string]any{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			out = append(out, ev)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, bufio.ErrTooLong) {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}
