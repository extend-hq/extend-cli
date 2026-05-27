package main

// On-disk layout for one runner invocation. Mirrors skill-creator's
// iteration-N convention so per-run artifacts are easy to navigate
// by humans and aggregable by the benchmark step:
//
//	<root>/iteration-<N>/
//	  eval-<id>/
//	    <harness>/                            (claude_code | codex)
//	      <config>/                           (with_skill | without_skill)
//	        run-<n>/
//	          events.jsonl                    raw harness JSONL
//	          extend-calls.jsonl              stub recording
//	          timing.json                     tokens + duration
//	          grading.json                    expectation results
//	          final.txt                       agent's final message
//	          scratch/                        agent's working dir
//	          home/                           agent's HOME (skill installed here)
//	          stub-bin/                       compiled stub on PATH
//	  benchmark.json                          aggregated rollup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// workspace is the top-level container for one runner invocation.
type workspace struct {
	Root      string // absolute path to the workspace root
	Iteration int    // iteration number (auto-incremented if --iteration unspecified)
}

// newWorkspace picks the next iteration number under root and prepares
// the iteration dir. Pass iter > 0 to force a specific iteration label.
func newWorkspace(root string, iter int) (*workspace, error) {
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
	w := &workspace{Root: abs, Iteration: iter}
	if err := os.MkdirAll(w.iterationDir(), 0o755); err != nil {
		return nil, err
	}
	return w, nil
}

// iterationDir is the iteration-N directory under Root.
func (w *workspace) iterationDir() string {
	return filepath.Join(w.Root, fmt.Sprintf("iteration-%d", w.Iteration))
}

// runDir returns the leaf dir for one harness × config × run-n
// invocation. Creates the dir and its scratch/home/stub-bin children.
func (w *workspace) runDir(evalID, harnessName, configName string, runN int) (string, error) {
	dir := filepath.Join(
		w.iterationDir(),
		"eval-"+evalID,
		pathSafe(harnessName), configName,
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

// benchmarkPath is where the aggregator writes the rollup.
func (w *workspace) benchmarkPath() string {
	return filepath.Join(w.iterationDir(), "benchmark.json")
}

// pathSafe replaces filesystem-hostile characters in a harness name
// (notably the ':' in model-pinned names like "claude_code:claude-sonnet-4-6")
// with '_' so artifact dirs stay portable across OSes and CI upload
// steps. The benchmark.json and console keep the original colon form.
func pathSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
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
