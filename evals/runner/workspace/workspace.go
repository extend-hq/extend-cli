// Package workspace owns the on-disk layout of a single eval run. The
// layout follows skill-creator's iteration-N convention so per-run
// artifacts are easy to navigate by humans and aggregable by the
// benchmark step:
//
//	<root>/iteration-<N>/
//	  eval-<id>/
//	    <harness>/                            (claude_code | codex)
//	      <mode>/                             (trigger | outcome)
//	        <config>/                         (with_skill | without_skill)
//	          run-<n>/
//	            events.jsonl                  raw harness JSONL
//	            extend-calls.jsonl            stub recording
//	            timing.json                   tokens + duration
//	            grading.json                  expectation results
//	            final.txt                     agent's final message
//	            scratch/                      agent's working dir
//	            home/                         agent's HOME (skill installed here)
//	            stub-bin/                     compiled stub on PATH
//	  benchmark.json                          aggregated rollup
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Workspace is the top-level container for one runner invocation.
type Workspace struct {
	Root      string // absolute path to the workspace root
	Iteration int    // iteration number (auto-incremented if --iteration unspecified)
}

// New picks the next iteration number under root and prepares the dir.
// Pass iter > 0 to force a specific iteration label.
func New(root string, iter int) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs %s: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", abs, err)
	}
	if iter == 0 {
		iter = nextIteration(abs)
	}
	w := &Workspace{Root: abs, Iteration: iter}
	if err := os.MkdirAll(w.IterationDir(), 0o755); err != nil {
		return nil, err
	}
	return w, nil
}

// IterationDir is the iteration-N directory under Root.
func (w *Workspace) IterationDir() string {
	return filepath.Join(w.Root, fmt.Sprintf("iteration-%d", w.Iteration))
}

// EvalDir returns the per-eval dir under the iteration.
func (w *Workspace) EvalDir(evalID string) string {
	return filepath.Join(w.IterationDir(), "eval-"+evalID)
}

// RunDir returns the leaf dir for one harness x mode x config x run-n
// invocation. Creates the dir if it doesn't exist.
func (w *Workspace) RunDir(evalID, harness, mode, config string, runN int) (string, error) {
	dir := filepath.Join(
		w.EvalDir(evalID),
		harness, mode, config,
		fmt.Sprintf("run-%d", runN),
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, sub := range []string{"scratch", "home", "stub-bin"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// BenchmarkPath is where the aggregator writes the rollup.
func (w *Workspace) BenchmarkPath() string {
	return filepath.Join(w.IterationDir(), "benchmark.json")
}

// nextIteration returns the lowest unused iteration number under root.
// Falls back to a millisecond-based label if scanning fails.
func nextIteration(root string) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return int(time.Now().UnixMilli() % 100000)
	}
	max := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		const prefix = "iteration-"
		if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		n, err := strconv.Atoi(name[len(prefix):])
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1
}
