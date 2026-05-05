package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/extend-hq/extend-cli/evals/runner/grade"
	"github.com/extend-hq/extend-cli/evals/runner/harness"
	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// benchmark aggregates per-(case × harness × mode × config) stats so
// the iteration's bench.json reports pass-rates and lets a human eyeball
// trends across iterations.
type benchmark struct {
	Iteration int                        `json:"iteration"`
	Cases     []*caseAggregate           `json:"cases"`
	Summary   benchmarkSummary           `json:"summary"`
	byKey     map[benchKey]*runAggregate `json:"-"`
	cases     map[string]*caseAggregate  `json:"-"`
}

type caseAggregate struct {
	ID       string          `json:"id"`
	Category string          `json:"category"`
	Path     string          `json:"path"`
	Runs     []*runAggregate `json:"runs"`
}

type runAggregate struct {
	Harness   string  `json:"harness"`
	Mode      string  `json:"mode"`
	Config    string  `json:"config"`
	Runs      int     `json:"runs"`
	PassedAvg float64 `json:"passed_avg"`  // passed / (passed+failed), excludes skipped
	Passed    int     `json:"passed"`      // total expectations passed across runs
	Failed    int     `json:"failed"`      // total expectations failed across runs
	Skipped   int     `json:"skipped"`     // judge expectations awaiting Phase 3
	Total     int     `json:"total"`       // passed + failed + skipped
	Tokens    int     `json:"tokens"`      // total tokens across runs (sum)
	Duration  int64   `json:"duration_ms"` // total wall-clock across runs
	Aborted   int     `json:"aborted"`     // count of aborted runs
}

type benchmarkSummary struct {
	TotalCases     int          `json:"total_cases"`
	TotalRuns      int          `json:"total_runs"`
	OverallPassPct float64      `json:"overall_pass_pct"`
	WithSkill      groupSummary `json:"with_skill"`
	WithoutSkill   groupSummary `json:"without_skill"`
}

type groupSummary struct {
	PassPct float64 `json:"pass_pct"`
	Tokens  int     `json:"tokens"`
	Skipped int     `json:"skipped"` // judge expectations awaiting Phase 3
}

type benchKey struct {
	caseID, harness, mode, config string
}

func newBenchmark() *benchmark {
	return &benchmark{
		byKey: map[benchKey]*runAggregate{},
		cases: map[string]*caseAggregate{},
	}
}

func (b *benchmark) add(e spec.Eval, harnessName, mode, configName string, r *harness.Result, gr []grade.Result) {
	if _, ok := b.cases[e.ID]; !ok {
		c := &caseAggregate{ID: e.ID, Category: e.Category, Path: string(e.Path)}
		b.cases[e.ID] = c
		b.Cases = append(b.Cases, c)
	}
	caseAgg := b.cases[e.ID]

	key := benchKey{e.ID, harnessName, mode, configName}
	agg, ok := b.byKey[key]
	if !ok {
		agg = &runAggregate{Harness: harnessName, Mode: mode, Config: configName}
		b.byKey[key] = agg
		caseAgg.Runs = append(caseAgg.Runs, agg)
	}
	agg.Runs++
	for _, x := range gr {
		agg.Total++
		switch {
		case x.Skipped:
			agg.Skipped++
		case x.Passed:
			agg.Passed++
		default:
			agg.Failed++
		}
	}
	graded := agg.Passed + agg.Failed
	if graded > 0 {
		agg.PassedAvg = float64(agg.Passed) / float64(graded)
	}
	if r.Tokens > 0 {
		agg.Tokens += r.Tokens
	}
	agg.Duration += r.DurationMS
	if r.Aborted {
		agg.Aborted++
	}
}

// finalize computes summary fields. Idempotent; safe to call multiple times.
// Pass-rates exclude skipped (i.e. unwired-judge) expectations: they're
// reported in the per-case detail but don't deflate the headline number.
func (b *benchmark) finalize() {
	stable := append([]*caseAggregate{}, b.Cases...)
	sort.Slice(stable, func(i, j int) bool { return stable[i].ID < stable[j].ID })
	b.Cases = stable

	withGraded, withPass, withSkipped, withTokens := 0, 0, 0, 0
	withoutGraded, withoutPass, withoutSkipped, withoutTokens := 0, 0, 0, 0
	totalRuns := 0

	for _, c := range b.Cases {
		for _, agg := range c.Runs {
			totalRuns += agg.Runs
			graded := agg.Passed + agg.Failed
			switch agg.Config {
			case "with_skill":
				withGraded += graded
				withPass += agg.Passed
				withSkipped += agg.Skipped
				withTokens += agg.Tokens
			case "without_skill":
				withoutGraded += graded
				withoutPass += agg.Passed
				withoutSkipped += agg.Skipped
				withoutTokens += agg.Tokens
			}
		}
	}
	b.Summary = benchmarkSummary{
		TotalCases:     len(b.Cases),
		TotalRuns:      totalRuns,
		WithSkill:      groupSummary{PassPct: pct(withPass, withGraded), Tokens: withTokens, Skipped: withSkipped},
		WithoutSkill:   groupSummary{PassPct: pct(withoutPass, withoutGraded), Tokens: withoutTokens, Skipped: withoutSkipped},
		OverallPassPct: pct(withPass+withoutPass, withGraded+withoutGraded),
	}
}

func (b *benchmark) write(path string) error {
	b.finalize()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

func (b *benchmark) printSummary() {
	b.finalize()
	fmt.Printf("=== Summary ===\n")
	fmt.Printf("Cases:        %d\n", b.Summary.TotalCases)
	fmt.Printf("Runs:         %d\n", b.Summary.TotalRuns)
	fmt.Printf("with_skill    pass-rate: %5.1f%%   tokens: %d   skipped: %d\n",
		b.Summary.WithSkill.PassPct, b.Summary.WithSkill.Tokens, b.Summary.WithSkill.Skipped)
	fmt.Printf("without_skill pass-rate: %5.1f%%   tokens: %d   skipped: %d\n",
		b.Summary.WithoutSkill.PassPct, b.Summary.WithoutSkill.Tokens, b.Summary.WithoutSkill.Skipped)
	if b.Summary.WithSkill.PassPct > 0 || b.Summary.WithoutSkill.PassPct > 0 {
		delta := b.Summary.WithSkill.PassPct - b.Summary.WithoutSkill.PassPct
		fmt.Printf("delta (with - without): %+.1fpp\n", delta)
	}
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return 100.0 * float64(num) / float64(denom)
}
