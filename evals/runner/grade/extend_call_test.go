package grade

import (
	"testing"

	"github.com/extend-hq/extend-cli/evals/runner/spec"
)

func TestCheckExtendCall_NotArgvPrefixExcludesSubcommands(t *testing.T) {
	noBareExtract := spec.Expectation{
		Type: spec.TypeExtendCall,
		MustNotContain: []spec.ExtendCallPredicate{{
			ArgvPrefix:      []string{"extract"},
			NotArgvPrefix:   [][]string{{"extract", "batch"}, {"extract", "batches"}, {"extract", "runs"}},
			MustNotHaveFlag: "help",
		}},
	}

	batchWorkflow := []CallRecord{
		{Argv: []string{"extract", "--help"}},
		{Argv: []string{"extract", "-h"}},
		{Argv: []string{"extract", "batch", "--help"}},
		{Argv: []string{"extract", "batch", "a.pdf", "b.pdf", "--using", "ex_1"}},
		{Argv: []string{"extract", "batches", "watch", "bpr_1"}},
		{Argv: []string{"extract", "runs", "list", "--batch", "bpr_1"}},
	}
	if ok, ev := checkExtendCall(noBareExtract, Inputs{Calls: batchWorkflow}); !ok {
		t.Errorf("batch workflow should pass, got fail: %s", ev)
	}

	for _, bad := range [][]string{
		{"extract", "a.pdf", "--using", "ex_1"},
		{"extract", "file_abc", "--using", "ex_1"},
		{"extract", "a.pdf", "b.pdf", "--package", "--using", "ex_1"},
	} {
		calls := append(append([]CallRecord{}, batchWorkflow...), CallRecord{Argv: bad})
		if ok, _ := checkExtendCall(noBareExtract, Inputs{Calls: calls}); ok {
			t.Errorf("bare extract call %v should have been rejected", bad)
		}
	}
}

func TestCountCalls_IgnoresHelpForNamedVerbs(t *testing.T) {
	calls := []CallRecord{
		{Argv: []string{"extract", "runs", "list", "--help"}},
		{Argv: []string{"extract", "runs", "list", "-h"}},
		{Argv: []string{"extract", "runs", "list", "--status", "FAILED"}},
	}
	if got := countCalls(calls, "extract.runs.list"); got != 1 {
		t.Errorf("countCalls(extract.runs.list) = %d, want 1 (excluding --help/-h)", got)
	}
	// Empty verbPath counts every invocation, including --help, for T-* cases.
	if got := countCalls(calls, ""); got != 3 {
		t.Errorf("countCalls(\"\") = %d, want 3", got)
	}
}
