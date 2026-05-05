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
	"sync"
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
		evalsPath   = flag.String("evals", "../evals.json", "path to evals.json")
		wsRoot      = flag.String("workspace", "", "workspace root (default: <repo>/../extend-cli-evals-workspace)")
		iter        = flag.Int("iteration", 0, "iteration number (default: next free)")
		caseList    = flag.String("cases", "", "comma-separated case IDs to run (default: all)")
		harnList    = flag.String("harnesses", "", "comma-separated harness names: claude_code,codex (default: all available)")
		runs        = flag.Int("runs", 1, "runs per (case, harness, mode, config) tuple")
		timeout     = flag.Duration("timeout", 5*time.Minute, "per-run timeout")
		concurrency = flag.Int("concurrency", 4, "max harness invocations to run in parallel")
		effort      = flag.String("effort", "low", "model effort/reasoning level: low|medium|high (low recommended for evals — tasks are simple and benefit from speed)")
		fastMode    = flag.Bool("fast", true, "enable harness fast modes (Codex service_tier=fast). Anthropic priority tier requires a sales contract and is not toggled here.")
		noJudge     = flag.Bool("no-judge", false, "skip LLM-judge expectations (no Anthropic API calls); judge expectations report Skipped")
		judgeModel  = flag.String("judge-model", "claude-opus-4-7", "model used by the LLM judge for `judge` expectations")
		judgeEffort = flag.String("judge-effort", "low", "effort level for the judge: low|medium|high|xhigh|max (low recommended; judging is a simple classification task)")
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

	// Build the full task list once. Each task is one (case × harness ×
	// mode × config × run-n) tuple — these are independent and can run
	// in parallel up to -concurrency.
	type task struct {
		eval   spec.Eval
		driver harness.Driver
		mode   string
		config string
		runN   int
	}
	var tasks []task
	for _, e := range cases {
		for _, d := range drivers {
			for _, mode := range e.Modes {
				for _, cfgName := range []string{"with_skill", "without_skill"} {
					for n := 1; n <= *runs; n++ {
						tasks = append(tasks, task{e, d, string(mode), cfgName, n})
					}
				}
			}
		}
	}

	tuneOpts := harness.TuneOptions{
		Effort:   *effort,
		FastMode: *fastMode,
	}

	judgeCfg := grade.JudgeFromEnv()
	judgeCfg.Model = *judgeModel
	judgeCfg.Effort = *judgeEffort
	if *noJudge {
		judgeCfg.Disabled = true
	}

	judgeNote := fmt.Sprintf("judge=%s effort=%s", judgeCfg.Model, judgeCfg.Effort)
	switch {
	case judgeCfg.Disabled:
		judgeNote = "judge=disabled"
	case judgeCfg.APIKey == "":
		judgeNote = "judge=skipped (ANTHROPIC_API_KEY not set)"
	}
	fmt.Printf("running %d tasks (%d cases × %d harness × configs × runs) at concurrency=%d  [%s]\n",
		len(tasks), len(cases), len(drivers), *concurrency, judgeNote)

	// printedCases keeps console output legible: as soon as we have any
	// result for a case, print its header once.
	var (
		mu           sync.Mutex
		printedCases = map[string]bool{}
	)

	taskCh := make(chan task)
	var wg sync.WaitGroup
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				r, gr, err := runOne(t.driver, t.eval, t.mode, t.config, t.runN, ws, stubBin, *timeout, tuneOpts, judgeCfg)
				mu.Lock()
				if !printedCases[t.eval.ID] {
					fmt.Printf("\n=== %s [%s] %s ===\n", t.eval.ID, t.eval.Category, summarizePrompt(t.eval.Prompt))
					printedCases[t.eval.ID] = true
				}
				if err != nil {
					log.Printf("  %s %s %s run-%d: ERROR %v", t.driver.Name(), t.mode, t.config, t.runN, err)
				} else {
					bench.add(t.eval, t.driver.Name(), t.mode, t.config, r, gr)
					printRun(t.driver.Name(), t.mode, t.config, t.runN, r, gr)
				}
				mu.Unlock()
			}
		}()
	}
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)
	wg.Wait()

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
	tune harness.TuneOptions,
	judgeCfg grade.JudgeConfig,
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
		Tune:             tune,
	}

	ctx := context.Background()
	res, err := d.Run(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	calls, _ := grade.LoadCalls(recordPath)
	results := grade.Grade(grade.Inputs{Eval: e, Harness: res, Calls: calls, Judge: judgeCfg})

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
	pass, fail, skip := 0, 0, 0
	for _, x := range gr {
		switch {
		case x.Skipped:
			skip++
		case x.Passed:
			pass++
		default:
			fail++
		}
	}
	graded := pass + fail
	abortMark := ""
	if r.Aborted {
		abortMark = " [TIMEOUT]"
	} else if r.ExitCode != 0 {
		abortMark = fmt.Sprintf(" [exit=%d]", r.ExitCode)
	}
	skipNote := ""
	if skip > 0 {
		skipNote = fmt.Sprintf(" (+%d skip)", skip)
	}
	fmt.Printf("  %-12s %-8s %-13s run-%d  %d/%d%s  %dms  %dt%s\n",
		harnessName, mode, cfgName, runN, pass, graded, skipNote, r.DurationMS, r.Tokens, abortMark)
}
