package cli

import (
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

func TestRunFailureError(t *testing.T) {
	cases := []struct {
		name    string
		reason  *string
		message *string
		want    string
	}{
		{
			name:    "reason and message",
			reason:  strptr("PRE_PROCESSING_FAILURE"),
			message: strptr("chunking failed"),
			want:    "run r_1 failed: PRE_PROCESSING_FAILURE: chunking failed",
		},
		{
			name:    "message only",
			reason:  nil,
			message: strptr("boom"),
			want:    "run r_1 failed: boom",
		},
		{
			name:    "reason only",
			reason:  strptr("FAILED_TO_PROCESS_FILE"),
			message: nil,
			want:    "run r_1 failed: FAILED_TO_PROCESS_FILE",
		},
		{
			name:    "neither (bare fallback)",
			reason:  nil,
			message: nil,
			want:    "run r_1 failed",
		},
		{
			// Empty-string pointers are treated as absent, not as a
			// literal ": " — the server can send "" for a field it
			// didn't populate.
			name:    "empty-string pointers fall back to bare",
			reason:  strptr(""),
			message: strptr(""),
			want:    "run r_1 failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runFailureError("r_1", tc.reason, tc.message).Error()
			if got != tc.want {
				t.Errorf("runFailureError = %q; want %q", got, tc.want)
			}
		})
	}
}

// TestRunFailureError_NoDoubleColon guards the specific regression where
// a nil reason but present message would render "run X failed: : msg".
func TestRunFailureError_NoDoubleColon(t *testing.T) {
	got := runFailureError("r_2", nil, strptr("only message")).Error()
	if strings.Contains(got, ": :") {
		t.Errorf("unexpected double colon in %q", got)
	}
}
