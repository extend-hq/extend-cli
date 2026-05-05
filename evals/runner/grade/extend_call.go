package grade

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

// checkExtendCall walks the recorded `extend` invocations and matches
// must_contain/must_not_contain/count_under/count_at_least.
//
// must_contain   — every predicate must match at least one call
// must_not_contain — no call may match any of these predicates
// count_under    — for each "verb path", call count < bound
// count_at_least — for each "verb path", call count >= bound
//
// All checks must pass for the expectation as a whole to pass; failures
// are concatenated into Evidence so the report explains every miss.
func checkExtendCall(exp spec.Expectation, in Inputs) (bool, string) {
	var fails []string

	for _, p := range exp.MustContain {
		if !anyCallMatches(in.Calls, p) {
			fails = append(fails, fmt.Sprintf("missing required call %s", describePredicate(p)))
		}
	}
	for _, p := range exp.MustNotContain {
		if anyCallMatches(in.Calls, p) {
			fails = append(fails, fmt.Sprintf("forbidden call observed %s", describePredicate(p)))
		}
	}
	for verbPath, bound := range exp.CountUnder {
		got := countCalls(in.Calls, verbPath)
		if got >= bound {
			fails = append(fails, fmt.Sprintf("count_under: %q seen %d times (bound %d)", verbPath, got, bound))
		}
	}
	for verbPath, bound := range exp.CountAtLeast {
		got := countCalls(in.Calls, verbPath)
		if got < bound {
			fails = append(fails, fmt.Sprintf("count_at_least: %q seen %d times (need %d)", verbPath, got, bound))
		}
	}

	if len(fails) == 0 {
		return true, fmt.Sprintf("matched against %d recorded call(s)", len(in.Calls))
	}
	return false, strings.Join(fails, "; ")
}

// anyCallMatches returns true if any recorded call satisfies p.
func anyCallMatches(calls []CallRecord, p spec.ExtendCallPredicate) bool {
	for _, c := range calls {
		if predicateMatches(p, c) {
			return true
		}
	}
	return false
}

// countCalls counts recorded calls whose positional verb path begins
// with the given dotted path (e.g. "extract" or "runs.list").
func countCalls(calls []CallRecord, verbPath string) int {
	want := strings.Split(verbPath, ".")
	if verbPath == "" {
		return len(calls)
	}
	n := 0
	for _, c := range calls {
		pos := positional(c.Argv)
		if hasVerbPrefix(pos, want) {
			n++
		}
	}
	return n
}

// predicateMatches checks one predicate against one call.
func predicateMatches(p spec.ExtendCallPredicate, c CallRecord) bool {
	pos := positional(c.Argv)

	if len(p.ArgvPrefix) > 0 {
		if !hasVerbPrefix(pos, p.ArgvPrefix) {
			return false
		}
	}
	for _, want := range p.Args {
		if !contains(c.Argv, want) {
			return false
		}
	}
	for _, want := range p.ArgsBasename {
		if !containsBasename(c.Argv, want) {
			return false
		}
	}
	if p.Flag != "" {
		if !hasFlag(c.Argv, p.Flag) {
			return false
		}
	}
	for k, v := range p.FlagValue {
		if got := flagValue(c.Argv, k); got != v {
			return false
		}
	}
	if p.MustNotHaveFlag != "" {
		if hasFlag(c.Argv, p.MustNotHaveFlag) {
			return false
		}
	}
	return true
}

// positional mirrors the stub's heuristic argv parser: extract leading
// non-flag tokens, skipping flag values.
func positional(args []string) []string {
	out := make([]string, 0, len(args))
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") {
				skip = true
			}
			continue
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			if !strings.Contains(a, "=") && len(a) == 2 {
				skip = true
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func hasVerbPrefix(pos, want []string) bool {
	if len(pos) < len(want) {
		return false
	}
	for i, w := range want {
		if pos[i] != w {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// containsBasename returns true if any token in haystack has its
// path basename equal to needle. Used by ExtendCallPredicate.ArgsBasename
// so an asserted "invoice.pdf" matches both relative and absolute forms.
func containsBasename(haystack []string, needle string) bool {
	for _, h := range haystack {
		if filepath.Base(h) == needle {
			return true
		}
	}
	return false
}

func hasFlag(args []string, name string) bool {
	prefix := "--" + name
	for _, a := range args {
		if a == prefix || strings.HasPrefix(a, prefix+"=") {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	prefix := "--" + name
	for i, a := range args {
		if a == prefix && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, prefix+"=") {
			return strings.TrimPrefix(a, prefix+"=")
		}
	}
	return ""
}
