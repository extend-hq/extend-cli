package cli

import (
	"errors"
	"fmt"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

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
