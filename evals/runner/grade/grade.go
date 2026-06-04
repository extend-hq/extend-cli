// Package grade evaluates a run's recorded evidence against an eval's
// expectations. Each expectation type has its own checker; results are
// the {text, passed, evidence} triple that the workspace's grading.json
// stores per the skill-creator schema.
//
// Phase 1: skill_activates, extend_call, stable_answer, and
// must_not_fabricate_ids are wired up. The judge expectation type is
// recognized but always returns Passed=false with an explanatory
// evidence string (Phase 2 wires it).
package grade

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/harness"
	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// Result is one expectation's verdict. Skipped expectations (e.g.
// judge expectations before the LLM-judge is wired in Phase 3) are
// reported but do not count toward pass-rate.
type Result struct {
	Text     string               `json:"text"`
	Passed   bool                 `json:"passed"`
	Skipped  bool                 `json:"skipped,omitempty"`
	Evidence string               `json:"evidence"`
	Type     spec.ExpectationType `json:"type"`
}

// CallRecord is one stub-recorded `extend` invocation. Mirrors the
// JSONL written by evals/stub/main.go's flushRecord() function. Each
// call's stdout/stderr is captured so the fabrication grader can
// scan for legitimate IDs without coupling to fixture details.
type CallRecord struct {
	TS       string   `json:"ts"`
	Argv     []string `json:"argv"`
	Mode     string   `json:"mode"`
	CWD      string   `json:"cwd"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

// Inputs is everything the grader needs to evaluate one run.
type Inputs struct {
	Eval    spec.Eval
	Harness *harness.Result
	Calls   []CallRecord // parsed extend-calls.jsonl
	Judge   JudgeConfig  // populated for judge expectations; zero-value/Disabled keeps them Skipped
}

// Grade evaluates every expectation in inputs.Eval and returns a
// per-expectation result list in source order. Errors during checking
// are surfaced as failed expectations with the error in Evidence; the
// grader is intentionally infallible at the top level.
func Grade(in Inputs) []Result {
	out := make([]Result, 0, len(in.Eval.Expectations))
	for _, exp := range in.Eval.Expectations {
		r := Result{Text: exp.Text, Type: exp.Type}
		if r.Text == "" {
			r.Text = autoText(exp)
		}
		switch exp.Type {
		case spec.TypeSkillActivates:
			r.Passed, r.Evidence = checkSkillActivates(exp, in)
		case spec.TypeExtendCall:
			r.Passed, r.Evidence = checkExtendCall(exp, in)
		case spec.TypeStableAnswer:
			r.Passed, r.Evidence = checkStableAnswer(exp, in)
		case spec.TypeMustNotFabricateIDs:
			r.Passed, r.Evidence = checkFabrication(exp, in)
		case spec.TypeJudge:
			passed, skipped, evidence := checkJudge(exp, in, in.Judge)
			r.Passed = passed
			r.Skipped = skipped
			r.Evidence = evidence
		default:
			r.Evidence = fmt.Sprintf("unknown type %q", exp.Type)
		}
		out = append(out, r)
	}
	return out
}

// LoadCalls reads and parses the stub's recorded JSONL.
func LoadCalls(path string) ([]CallRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no calls is a valid (and informative) state
		}
		return nil, err
	}
	out := []CallRecord{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c CallRecord
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue // malformed lines are skipped silently
		}
		out = append(out, c)
	}
	return out, nil
}

// autoText synthesizes a default Text for an expectation with no
// authored label, so reports never have blank rows.
func autoText(e spec.Expectation) string {
	switch e.Type {
	case spec.TypeSkillActivates:
		return fmt.Sprintf("skill %q activated", e.Skill)
	case spec.TypeExtendCall:
		switch {
		case len(e.MustContain) > 0:
			return "extend invoked: " + describePredicate(e.MustContain[0])
		case len(e.MustNotContain) > 0:
			return "extend NOT invoked: " + describePredicate(e.MustNotContain[0])
		case len(e.CountAtLeast) > 0:
			for k, v := range e.CountAtLeast {
				return fmt.Sprintf("at least %d call(s) to %q", v, k)
			}
		case len(e.CountUnder) > 0:
			for k, v := range e.CountUnder {
				return fmt.Sprintf("fewer than %d call(s) to %q", v, k)
			}
		}
		return "extend_call expectation"
	case spec.TypeStableAnswer:
		if e.Criterion != "" {
			return e.Criterion
		}
		if e.AnswerSubstr != "" {
			return "answer contains: " + e.AnswerSubstr
		}
		if e.AnswerPattern != "" {
			return "answer matches: " + e.AnswerPattern
		}
	case spec.TypeMustNotFabricateIDs:
		return "no fabricated IDs"
	case spec.TypeJudge:
		return e.Criterion
	}
	return string(e.Type)
}

func describePredicate(p spec.ExtendCallPredicate) string {
	parts := []string{}
	if len(p.ArgvPrefix) > 0 {
		parts = append(parts, "argv["+strings.Join(p.ArgvPrefix, " ")+"]")
	}
	if len(p.Args) > 0 {
		parts = append(parts, "args["+strings.Join(p.Args, " ")+"]")
	}
	if p.Flag != "" {
		parts = append(parts, "--"+p.Flag)
	}
	appendMap := func(label string, m map[string]string) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s[%s=%s]", label, k, m[k]))
		}
	}
	appendMap("flag", p.FlagValue)
	appendMap("flag_suffix", p.FlagValueSuffix)
	if p.MustNotHaveFlag != "" {
		parts = append(parts, "no --"+p.MustNotHaveFlag)
	}
	if p.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit[%d]", *p.ExitCode))
	}
	return strings.Join(parts, " ")
}

// WriteGradingJSON serializes a result list to a path.
func WriteGradingJSON(path string, results []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"expectations": results})
}
