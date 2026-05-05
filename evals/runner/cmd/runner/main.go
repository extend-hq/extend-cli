// Command runner orchestrates one full skill-evals iteration: build
// the stub binary, materialize fixtures, drive each (case × harness ×
// mode × config × run) tuple through its harness driver, grade the
// result, and emit a summary.
//
// Usage:
//
//	cd evals/runner
//	go run ./cmd/runner [flags]
//
// Flags
//
//	-evals      path to evals.json (default ../evals.json)
//	-workspace  workspace root (default ../../../extend-cli-evals-workspace)
//	-iteration  iteration number (default: next free under workspace)
//	-cases      comma-separated case IDs to run (default: all)
//	-harnesses  comma-separated harness names: claude_code,codex (default: all available)
//	-runs       runs per (case, harness, mode, config) tuple (default 1)
//	-timeout    per-run timeout (default 5m)
//
// Exit code: 0 if every run produced a graded result; 1 if any harness
// invocation failed catastrophically (exit codes from the harness
// itself are reported but do not fail the runner).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/extend-hq/extend-cli/evals/runner/fixtures"
	"github.com/extend-hq/extend-cli/evals/runner/grade"
	"github.com/extend-hq/extend-cli/evals/runner/harness"
	"github.com/extend-hq/extend-cli/evals/runner/spec"
	"github.com/extend-hq/extend-cli/evals/runner/workspace"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("runner: %v", err)
	}
}

func run() error {
	var (
		evalsPath = flag.String("evals", "../evals.json", "path to evals.json")
		wsRoot    = flag.String("workspace", "", "workspace root (default: <repo>/../extend-cli-evals-workspace)")
		iter      = flag.Int("iteration", 0, "iteration number (default: next free)")
		caseList  = flag.String("cases", "", "comma-separated case IDs to run (default: all)")
		harnList  = flag.String("harnesses", "", "comma-separated harness names: claude_code,codex (default: all available)")
		runs      = flag.Int("runs", 1, "runs per (case, harness, mode, config) tuple")
		timeout   = flag.Duration("timeout", 5*time.Minute, "per-run timeout")
	)
	flag.Parse()

	cfg, err := spec.Load(*evalsPath)
	if err != nil {
		return err
	}

	root := *wsRoot
	if root == "" {
		root = defaultWorkspaceRoot()
	}
	ws, err := workspace.New(root, *iter)
	if err != nil {
		return err
	}
	fmt.Printf("workspace: %s\n", ws.IterationDir())

	// Build the stub binary once into a stable location; we'll add it
	// to PATH for every harness invocation by symlinking into each
	// run's stub-bin/ dir as `extend`.
	stubBin, err := buildStub(ws.IterationDir())
	if err != nil {
		return fmt.Errorf("build stub: %w", err)
	}
	fmt.Printf("stub: %s\n", stubBin)

	drivers := pickHarnesses(*harnList)
	if len(drivers) == 0 {
		return fmt.Errorf("no available harnesses (install with `mise install`?)")
	}

	cases := pickCases(cfg.Evals, *caseList)
	if len(cases) == 0 {
		return fmt.Errorf("no cases match -cases=%q", *caseList)
	}

	bench := newBenchmark()

	for _, e := range cases {
		fmt.Printf("\n=== %s [%s] %s ===\n", e.ID, e.Category, summarizePrompt(e.Prompt))
		for _, d := range drivers {
			for _, mode := range e.Modes {
				for _, cfgName := range []string{"with_skill", "without_skill"} {
					for n := 1; n <= *runs; n++ {
						r, gr, err := runOne(d, e, string(mode), cfgName, n, ws, stubBin, *timeout)
						if err != nil {
							log.Printf("  %s %s %s run-%d: ERROR %v", d.Name(), mode, cfgName, n, err)
							continue
						}
						bench.add(e, d.Name(), string(mode), cfgName, r, gr)
						printRun(d.Name(), string(mode), cfgName, n, r, gr)
					}
				}
			}
		}
	}

	if err := bench.write(ws.BenchmarkPath()); err != nil {
		return fmt.Errorf("write benchmark: %w", err)
	}
	fmt.Println()
	bench.printSummary()
	fmt.Printf("\nbenchmark: %s\n", ws.BenchmarkPath())
	return nil
}

// runOne handles the workspace setup, harness invocation, and grading
// for one (case, harness, mode, config, run) tuple.
func runOne(
	d harness.Driver,
	e spec.Eval,
	mode, configName string, runN int,
	ws *workspace.Workspace,
	stubBin string,
	timeout time.Duration,
) (*harness.Result, []grade.Result, error) {
	dir, err := ws.RunDir(e.ID, d.Name(), mode, configName, runN)
	if err != nil {
		return nil, nil, err
	}
	scratch := filepath.Join(dir, "scratch")
	home := filepath.Join(dir, "home")
	stubDir := filepath.Join(dir, "stub-bin")
	recordPath := filepath.Join(dir, "extend-calls.jsonl")
	eventsPath := filepath.Join(dir, "events.jsonl")
	finalPath := filepath.Join(dir, "final.txt")
	gradingPath := filepath.Join(dir, "grading.json")
	timingPath := filepath.Join(dir, "timing.json")

	// Place the prebuilt stub on PATH as `extend` for this run.
	stubLink := filepath.Join(stubDir, "extend")
	_ = os.Remove(stubLink)
	if err := os.Symlink(stubBin, stubLink); err != nil {
		return nil, nil, fmt.Errorf("symlink stub: %w", err)
	}

	// Stage fixtures into scratch.
	if len(e.Files) > 0 {
		names := make([]string, len(e.Files))
		for i, f := range e.Files {
			names[i] = filepath.Base(f)
		}
		if _, err := fixtures.Materialize(scratch, names); err != nil {
			return nil, nil, fmt.Errorf("materialize fixtures: %w", err)
		}
	}

	stubMode := e.StubConfig.DefaultMode
	if stubMode == "" {
		stubMode = "real_responses"
	}

	opts := harness.RunOptions{
		Prompt:           e.Prompt,
		ScratchDir:       scratch,
		HomeDir:          home,
		StubDir:          stubDir,
		SkillEnabled:     configName == "with_skill",
		Mode:             mode,
		EventsPath:       eventsPath,
		FinalMessagePath: finalPath,
		RecordPath:       recordPath,
		Timeout:          timeout,
		StubMode:         stubMode,
	}

	ctx := context.Background()
	res, err := d.Run(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	calls, _ := grade.LoadCalls(recordPath)
	results := grade.Grade(grade.Inputs{Eval: e, Harness: res, Calls: calls})

	if err := grade.WriteGradingJSON(gradingPath, results); err != nil {
		return nil, nil, fmt.Errorf("write grading: %w", err)
	}
	if err := writeTimingJSON(timingPath, res); err != nil {
		return nil, nil, fmt.Errorf("write timing: %w", err)
	}

	return res, results, nil
}

func writeTimingJSON(path string, r *harness.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"harness":     r.HarnessName,
		"duration_ms": r.DurationMS,
		"tokens":      r.Tokens,
		"exit_code":   r.ExitCode,
		"aborted":     r.Aborted,
		"skill_read":  r.SkillRead,
	})
}

// buildStub compiles evals/stub from the parent module into a binary
// inside the iteration dir. The binary path is then symlinked into
// each run's stub-bin/ dir.
func buildStub(iterDir string) (string, error) {
	bin := filepath.Join(iterDir, "stub")
	cmd := exec.Command("go", "build", "-o", bin, "./evals/stub")
	cmd.Dir = repoRoot()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return bin, nil
}

// pickHarnesses returns drivers for every harness that's both
// available on PATH and (if -harnesses was set) requested by name.
func pickHarnesses(filter string) []harness.Driver {
	all := []harness.Driver{&harness.Claude{}, &harness.Codex{}}
	wants := map[string]bool{}
	if filter != "" {
		for _, h := range strings.Split(filter, ",") {
			wants[strings.TrimSpace(h)] = true
		}
	}
	out := []harness.Driver{}
	for _, d := range all {
		if len(wants) > 0 && !wants[d.Name()] {
			continue
		}
		if err := d.Available(); err != nil {
			log.Printf("skipping %s: %v", d.Name(), err)
			continue
		}
		out = append(out, d)
	}
	return out
}

// pickCases filters cases by ID. Empty filter = all cases.
func pickCases(all []spec.Eval, filter string) []spec.Eval {
	if filter == "" {
		return all
	}
	want := map[string]bool{}
	for _, c := range strings.Split(filter, ",") {
		want[strings.TrimSpace(c)] = true
	}
	out := []spec.Eval{}
	for _, e := range all {
		if want[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

// summarizePrompt returns a short one-line excerpt of the prompt.
func summarizePrompt(p string) string {
	p = strings.Join(strings.Fields(p), " ")
	if len(p) > 80 {
		p = p[:77] + "..."
	}
	return p
}

func defaultWorkspaceRoot() string {
	return filepath.Join(repoRoot(), "..", "extend-cli-evals-workspace")
}

func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cur := wd
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return wd
		}
		mod := filepath.Join(parent, "go.mod")
		if b, err := os.ReadFile(mod); err == nil &&
			strings.Contains(string(b), "module github.com/extend-hq/extend-cli\n") {
			return parent
		}
		cur = parent
	}
}

func printRun(harnessName, mode, cfgName string, runN int, r *harness.Result, gr []grade.Result) {
	pass, total := 0, 0
	for _, x := range gr {
		total++
		if x.Passed {
			pass++
		}
	}
	abortMark := ""
	if r.Aborted {
		abortMark = " [TIMEOUT]"
	} else if r.ExitCode != 0 {
		abortMark = fmt.Sprintf(" [exit=%d]", r.ExitCode)
	}
	fmt.Printf("  %-12s %-8s %-13s run-%d  %d/%d  %dms  %dt%s\n",
		harnessName, mode, cfgName, runN, pass, total, r.DurationMS, r.Tokens, abortMark)
}
