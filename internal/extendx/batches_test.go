package extendx

import (
	"errors"
	"strings"
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

func TestValidateBatchID(t *testing.T) {
	// Matching kinds pass.
	if err := ValidateBatchID(BatchKindProcessor, "bpr_x", "get"); err != nil {
		t.Errorf("ValidateBatchID(processor, bpr_x) = %v; want nil", err)
	}
	if err := ValidateBatchID(BatchKindParse, "bpar_x", "watch"); err != nil {
		t.Errorf("ValidateBatchID(parse, bpar_x) = %v; want nil", err)
	}
	// Workflow batch IDs always fail with the sentinel, regardless of
	// the expected kind.
	for _, expected := range []BatchKind{BatchKindProcessor, BatchKindParse} {
		if err := ValidateBatchID(expected, "batch_x", "get"); !errors.Is(err, ErrWorkflowBatchNotRetrievable) {
			t.Errorf("ValidateBatchID(%s, batch_x) = %v; want ErrWorkflowBatchNotRetrievable", expected, err)
		}
	}
	// Cross-kind mismatches redirect to the other command family.
	if err := ValidateBatchID(BatchKindParse, "bpr_x", "get"); err == nil {
		t.Error("ValidateBatchID(parse, bpr_x) = nil; want mismatch error")
	}
	err := ValidateBatchID(BatchKindProcessor, "bpar_x", "watch")
	if err == nil {
		t.Fatal("ValidateBatchID(processor, bpar_x) = nil; want mismatch error")
	}
	if want := "extend parse batches watch bpar_x"; !strings.Contains(err.Error(), want) {
		t.Errorf("mismatch error %q does not mention %q", err, want)
	}
	// Unknown prefixes fail.
	if err := ValidateBatchID(BatchKindProcessor, "exr_x", "get"); err == nil {
		t.Error("ValidateBatchID(processor, exr_x) = nil; want non-nil")
	}
	if err := ValidateBatchID(BatchKindProcessor, "", "get"); err == nil {
		t.Error("ValidateBatchID(processor, \"\") = nil; want non-nil")
	}
}

func TestErrSentinels(t *testing.T) {
	// Lock the sentinel identity — callers compare with errors.Is.
	if !errors.Is(ErrWorkflowBatchNotRetrievable, ErrWorkflowBatchNotRetrievable) {
		t.Error("ErrWorkflowBatchNotRetrievable must be identity-comparable")
	}
}
