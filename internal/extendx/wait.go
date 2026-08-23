package extendx

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// WaitOptions controls the polling cadence used by Wait* helpers.
// Zero-valued Interval/MaxInterval fall back to ProfileShort.
type WaitOptions struct {
	Interval    time.Duration
	MaxInterval time.Duration
	Timeout     time.Duration
}

// WaitTimeoutError is returned by the wait helpers when the configured
// Timeout elapses before the run reaches a terminal state. It is
// distinct from a parent-context cancellation (which surfaces the
// parent's err instead) so callers can tell "user gave up" from "we
// hit our own clock" and render an actionable retry hint.
type WaitTimeoutError struct {
	// Timeout is the budget that was exceeded.
	Timeout time.Duration
}

func (e *WaitTimeoutError) Error() string {
	return fmt.Sprintf("wait timed out after %s without reaching a terminal state", e.Timeout)
}

// Is lets errors.Is(err, context.DeadlineExceeded) match this typed
// error. Existing callers that only check for DeadlineExceeded keep
// working; new callers can errors.As to grab the timeout duration for
// richer messages.
func (e *WaitTimeoutError) Is(target error) bool {
	return target == context.DeadlineExceeded
}

// WaitProfile names a polling cadence used by long-running commands.
//
// The CLI runs different operations against profiles tuned for their
// typical durations: short actions (extract, classify, split, parse,
// edit) complete in seconds and use ProfileShort, while workflow runs
// and batches can run for minutes to hours and use ProfileLong, which
// polls less aggressively to reduce server load and rate-limit
// pressure.
//
// This is the canonical source for the polling values; help topics
// render the table from here so documentation stays in sync with
// behavior.
type WaitProfile string

const (
	// ProfileShort: 1s -> 10s. Used by extract, classify, split, parse,
	// edit, and `runs watch` on those run kinds.
	ProfileShort WaitProfile = "short"
	// ProfileLong: 2s -> 30s. Used by workflow runs (`extend run`) and
	// batch watching (`extend <verb> batches watch`).
	ProfileLong WaitProfile = "long"
)

// waitProfileTable holds the per-profile polling parameters. Keep this
// the single source of truth; help-topic rendering reads it.
var waitProfileTable = map[WaitProfile]struct {
	Interval, MaxInterval time.Duration
}{
	ProfileShort: {Interval: 1 * time.Second, MaxInterval: 10 * time.Second},
	ProfileLong:  {Interval: 2 * time.Second, MaxInterval: 30 * time.Second},
}

// WaitProfileOptions returns WaitOptions for the named profile, with
// the caller's timeout applied. Unknown profiles fall back to
// ProfileShort so callers never get a zero-valued (busy-loop)
// WaitOptions.
func WaitProfileOptions(p WaitProfile, timeout time.Duration) WaitOptions {
	row, ok := waitProfileTable[p]
	if !ok {
		row = waitProfileTable[ProfileShort]
	}
	return WaitOptions{Interval: row.Interval, MaxInterval: row.MaxInterval, Timeout: timeout}
}

// WaitProfileSpec is the public shape of a profile's parameters,
// exposed for documentation rendering.
type WaitProfileSpec struct {
	Profile     WaitProfile
	Interval    time.Duration
	MaxInterval time.Duration
}

// WaitProfileSpecs returns every registered profile, in a stable order
// suitable for rendering in help topics.
func WaitProfileSpecs() []WaitProfileSpec {
	order := []WaitProfile{ProfileShort, ProfileLong}
	out := make([]WaitProfileSpec, 0, len(order))
	for _, p := range order {
		row := waitProfileTable[p]
		out = append(out, WaitProfileSpec{Profile: p, Interval: row.Interval, MaxInterval: row.MaxInterval})
	}
	return out
}

// PollForRun drives a get/poll loop. `get` is invoked once per
// iteration to fetch the current run state; `statusOf` extracts a
// canonical RunStatus from whatever shape `get` returns. The loop
// terminates when statusOf reports a terminal state, when the parent
// context is cancelled, or when opts.Timeout elapses. `onPoll`, if
// non-nil, is called with the just-fetched value before the terminal
// check so callers can update spinners.
//
// This is generic so each command can supply its own getter that
// returns *extend.ExtractRun / *extend.ParseRun / ... without
// allocations or interface boxing.
func PollForRun[T any](
	parentCtx context.Context,
	get func(context.Context) (T, error),
	statusOf func(T) RunStatus,
	opts WaitOptions,
	onPoll func(T),
) (T, error) {
	opts = applyWaitDefaults(opts)
	ctx := parentCtx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parentCtx, opts.Timeout)
		defer cancel()
	}
	var zero T
	delay := opts.Interval
	for {
		run, err := get(ctx)
		if err != nil {
			return zero, classifyWaitErr(err, parentCtx, opts.Timeout)
		}
		if onPoll != nil {
			onPoll(run)
		}
		if statusOf(run).IsTerminal() {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return zero, classifyWaitErr(ctx.Err(), parentCtx, opts.Timeout)
		case <-time.After(delay):
		}
		if delay < opts.MaxInterval {
			delay = min(delay*5/4, opts.MaxInterval)
		}
	}
}

// classifyWaitErr maps a raw error from a polling iteration into either
// the typed WaitTimeoutError (when we hit our own timeout) or the
// original error (parent cancel, transport failure, API error). It's
// called from both the `get` path and the `ctx.Done()` arm so a slow
// API call that happens to hit the deadline first still surfaces as a
// wait timeout.
func classifyWaitErr(err error, parentCtx context.Context, timeout time.Duration) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) && parentCtx.Err() == nil && timeout > 0 {
		return &WaitTimeoutError{Timeout: timeout}
	}
	return err
}

func applyWaitDefaults(opts WaitOptions) WaitOptions {
	short := waitProfileTable[ProfileShort]
	if opts.Interval <= 0 {
		opts.Interval = short.Interval
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = short.MaxInterval
	}
	return opts
}
