package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// runFailureError builds the error returned when a run reaches a
// terminal FAILED state. It folds the server's coded failureReason
// (e.g. PRE_PROCESSING_FAILURE) and the human-readable failureMessage
// into the message when present, so the stderr "Error:" line is
// self-diagnosing instead of a bare "run X failed". Both fields are
// *string on every run type; nil/empty entries are skipped, and when
// neither is set we fall back to the bare form.
func runFailureError(id string, reason, message *string) error {
	var parts []string
	if reason != nil && *reason != "" {
		parts = append(parts, *reason)
	}
	if message != nil && *message != "" {
		parts = append(parts, *message)
	}
	if len(parts) > 0 {
		return fmt.Errorf("run %s failed: %s", id, strings.Join(parts, ": "))
	}
	return fmt.Errorf("run %s failed", id)
}

// formatActionWaitError wraps an error from a wait helper into the
// message we want to show users of an action verb (extract, classify,
// parse, split, edit, run). The fmt.Errorf "wait: %w" pattern that
// used to live in every caller is preserved for non-timeout errors so
// unrelated failures (transport, API errors, parent cancel) keep their
// existing surface.
//
// For *extendx.WaitTimeoutError we render an actionable message that
// names the elapsed --timeout and the two recovery paths: raise
// --timeout, or detach with --wait=false and follow up with
// `extend runs watch <id>`. Without this guidance, agents tend to
// silently retry the same blocking command (as observed in the
// agent-experience transcripts) instead of switching to the cheap
// async path.
func formatActionWaitError(err error, runID string) error {
	if err == nil {
		return nil
	}
	var wt *extendx.WaitTimeoutError
	if errors.As(err, &wt) {
		return fmt.Errorf("run %s did not finish within --timeout %s; rerun with a larger --timeout, or detach with --wait=false and poll using 'extend runs watch %s'",
			runID, wt.Timeout, runID)
	}
	return fmt.Errorf("wait: %w", err)
}

// formatWatchWaitError is the runs-watch / batches-watch specialization.
// There is no --wait=false alternative here (the command itself is the
// polling loop), so the actionable hint just nudges the user toward a
// larger --timeout on retry.
func formatWatchWaitError(err error, id string) error {
	if err == nil {
		return nil
	}
	var wt *extendx.WaitTimeoutError
	if errors.As(err, &wt) {
		return fmt.Errorf("run %s still not in a terminal state after --timeout %s; rerun 'extend runs watch %s --timeout <larger>'",
			id, wt.Timeout, id)
	}
	return fmt.Errorf("wait: %w", err)
}
