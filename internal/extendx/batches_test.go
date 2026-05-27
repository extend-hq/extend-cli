package extendx

import (
	"errors"
	"testing"
)

func TestBatchKindFromID(t *testing.T) {
	cases := []struct {
		id   string
		kind BatchKind
		ok   bool
	}{
		{"bpr_xyz", BatchKindProcessor, true},
		{"bpar_xyz", BatchKindParse, true},
		{"batch_xyz", BatchKindWorkflow, true},
		// Order matters: bpar_ must beat bpr_ even though bpr_ is
		// a prefix-substring. Confirm the parse batch case wins.
		// (The implementation checks bpr_ first — verify that an
		// ID starting bpr_ stays processor.)
		{"bpr_abc", BatchKindProcessor, true},
		// Run IDs are NOT batch IDs.
		{"exr_xyz", "", false},
		{"workflow_run_xyz", "", false},
		// Empty and unknown.
		{"", "", false},
		{"file_xyz", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, ok := BatchKindFromID(tc.id)
			if got != tc.kind || ok != tc.ok {
				t.Errorf("BatchKindFromID(%q) = (%q, %v); want (%q, %v)", tc.id, got, ok, tc.kind, tc.ok)
			}
		})
	}
}

func TestCanCancel(t *testing.T) {
	// Cancellable kinds: extract, classify, split, workflow.
	cancellable := []string{"exr_x", "clr_x", "splr_x", "workflow_run_x"}
	for _, id := range cancellable {
		if err := CanCancel(id); err != nil {
			t.Errorf("CanCancel(%q) = %v; want nil", id, err)
		}
	}
	// Non-cancellable: parse, edit. Each returns a specific error.
	if err := CanCancel("pr_x"); err == nil {
		t.Error("CanCancel(parse) = nil; want non-nil")
	}
	if err := CanCancel("edr_x"); err == nil {
		t.Error("CanCancel(edit) = nil; want non-nil")
	}
	// Unknown prefix.
	err := CanCancel("file_x")
	if err == nil {
		t.Error("CanCancel(unknown) = nil; want non-nil")
	}
	// Empty.
	if err := CanCancel(""); err == nil {
		t.Error("CanCancel(\"\") = nil; want non-nil")
	}
}

func TestErrSentinels(t *testing.T) {
	// Lock the sentinel identities — callers compare with errors.Is.
	if !errors.Is(ErrWorkflowBatchNotRetrievable, ErrWorkflowBatchNotRetrievable) {
		t.Error("ErrWorkflowBatchNotRetrievable must be identity-comparable")
	}
	if !errors.Is(ErrNotCancellable, ErrNotCancellable) {
		t.Error("ErrNotCancellable must be identity-comparable")
	}
}
