package extendx

import (
	"strings"
	"testing"
)

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

func TestValidateRunID(t *testing.T) {
	// Matching kind and prefix passes for every kind.
	matching := map[RunKind]string{
		KindExtract:    "exr_x",
		KindParse:      "pr_x",
		KindClassify:   "clr_x",
		KindSplit:      "splr_x",
		KindWorkflow:   "workflow_run_x",
		KindEdit:       "edr_x",
		KindDetectForm: "sgr_x",
	}
	for kind, id := range matching {
		if err := ValidateRunID(kind, id, "get"); err != nil {
			t.Errorf("ValidateRunID(%s, %q) = %v; want nil", kind, id, err)
		}
	}
	// A mismatched ID redirects to the owning command group.
	err := ValidateRunID(KindParse, "exr_x", "get")
	if err == nil {
		t.Fatal("ValidateRunID(parse, exr_x) = nil; want mismatch error")
	}
	if want := "extend extract runs get exr_x"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	// Workflow runs redirect to the plural group.
	err = ValidateRunID(KindExtract, "workflow_run_x", "watch")
	if err == nil {
		t.Fatal("ValidateRunID(extract, workflow_run_x) = nil; want mismatch error")
	}
	if want := "extend workflows runs watch workflow_run_x"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	// When the ID's actual kind lacks the invoked action, the error
	// must not suggest a command that doesn't exist.
	err = ValidateRunID(KindExtract, "pr_x", "cancel")
	if err == nil {
		t.Fatal("ValidateRunID(extract, pr_x, cancel) = nil; want mismatch error")
	}
	if strings.Contains(err.Error(), "extend parse runs cancel") {
		t.Errorf("mismatch error %q suggests nonexistent 'parse runs cancel'", err)
	}
	if want := "parse runs do not support cancel"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	err = ValidateRunID(KindWorkflow, "exr_x", "update")
	if err == nil {
		t.Fatal("ValidateRunID(workflow, exr_x, update) = nil; want mismatch error")
	}
	if want := "extract runs do not support update"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	// A form-detection ID pasted into another group redirects to
	// detect-form for supported actions and explains for unsupported.
	err = ValidateRunID(KindEdit, "sgr_x", "get")
	if err == nil {
		t.Fatal("ValidateRunID(edit, sgr_x, get) = nil; want mismatch error")
	}
	if want := "extend detect-form runs get sgr_x"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	err = ValidateRunID(KindExtract, "sgr_x", "delete")
	if err == nil {
		t.Fatal("ValidateRunID(extract, sgr_x, delete) = nil; want mismatch error")
	}
	if want := "detect-form runs do not support delete"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	// Unknown prefixes fail.
	if err := ValidateRunID(KindExtract, "file_x", "get"); err == nil {
		t.Error("ValidateRunID(extract, file_x) = nil; want non-nil")
	}
	if err := ValidateRunID(KindExtract, "", "get"); err == nil {
		t.Error("ValidateRunID(extract, \"\") = nil; want non-nil")
	}
}

func TestSupportsRunAction(t *testing.T) {
	cases := []struct {
		kind   RunKind
		action string
		want   bool
	}{
		{KindParse, "cancel", false},
		{KindEdit, "cancel", false},
		{KindExtract, "cancel", true},
		{KindWorkflow, "cancel", true},
		{KindEdit, "list", false},
		{KindParse, "list", true},
		{KindWorkflow, "update", true},
		{KindExtract, "update", false},
		{KindEdit, "get", true},
		{KindEdit, "watch", true},
		{KindEdit, "delete", true},
		{KindDetectForm, "get", true},
		{KindDetectForm, "watch", true},
		{KindDetectForm, "list", false},
		{KindDetectForm, "cancel", false},
		{KindDetectForm, "delete", false},
		{KindDetectForm, "update", false},
	}
	for _, tc := range cases {
		if got := SupportsRunAction(tc.kind, tc.action); got != tc.want {
			t.Errorf("SupportsRunAction(%s, %s) = %v; want %v", tc.kind, tc.action, got, tc.want)
		}
	}
}

func TestRunKindVerb(t *testing.T) {
	if got := KindWorkflow.Verb(); got != "workflows" {
		t.Errorf("KindWorkflow.Verb() = %q; want workflows", got)
	}
	for _, k := range []RunKind{KindExtract, KindParse, KindClassify, KindSplit, KindEdit} {
		if got := k.Verb(); got != string(k) {
			t.Errorf("%s.Verb() = %q; want %q", k, got, string(k))
		}
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
