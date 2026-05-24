package extendx

import "testing"

func TestRunKindFromID(t *testing.T) {
	cases := []struct {
		id   string
		kind RunKind
		ok   bool
	}{
		{"exr_xyz", KindExtract, true},
		{"pr_xyz", KindParse, true},
		{"clr_xyz", KindClassify, true},
		{"splr_xyz", KindSplit, true},
		{"workflow_run_xyz", KindWorkflow, true},
		{"edr_xyz", KindEdit, true},
		// Workflow batch IDs and processor batch IDs are NOT run
		// IDs — they belong to BatchKindFromID and must not match.
		{"batch_xyz", "", false},
		{"bpr_xyz", "", false},
		{"bpar_xyz", "", false},
		// Empty and unknown prefixes return ("", false).
		{"", "", false},
		{"file_xyz", "", false},
		// Edge: `pr_` prefix must not eat `pr` substring inside
		// other IDs. The function uses HasPrefix so this is safe,
		// but we lock the contract.
		{"workflow_pr_xyz", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, ok := RunKindFromID(tc.id)
			if got != tc.kind || ok != tc.ok {
				t.Errorf("RunKindFromID(%q) = (%q, %v); want (%q, %v)", tc.id, got, ok, tc.kind, tc.ok)
			}
		})
	}
}

func TestRunStatus_IsTerminal(t *testing.T) {
	terminal := []RunStatus{
		StatusProcessed, StatusFailed, StatusCancelled,
		StatusNeedsReview, StatusRejected,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("RunStatus(%q).IsTerminal() = false; want true", s)
		}
	}
	nonTerminal := []RunStatus{
		StatusPending, StatusProcessing, StatusCancelling,
		RunStatus("UNKNOWN"), RunStatus(""),
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("RunStatus(%q).IsTerminal() = true; want false", s)
		}
	}
}

func TestRunStatus_IsTerminalFailure(t *testing.T) {
	// NEEDS_REVIEW is terminal but not failure — explicit guard so
	// the exit-code semantics stay stable.
	if StatusNeedsReview.IsTerminalFailure() {
		t.Error("NEEDS_REVIEW must not be a terminal failure")
	}
	if StatusProcessed.IsTerminalFailure() {
		t.Error("PROCESSED must not be a terminal failure")
	}
	for _, s := range []RunStatus{StatusFailed, StatusCancelled, StatusRejected} {
		if !s.IsTerminalFailure() {
			t.Errorf("%q must be a terminal failure", s)
		}
	}
}
