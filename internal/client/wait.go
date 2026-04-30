package client

import "time"

// WaitProfile names a polling cadence used by long-running commands.
//
// The CLI runs different operations against profiles tuned for their typical
// durations: short actions (extract, classify, split, parse, edit) complete
// in seconds and use ProfileShort, while workflow runs and batches can run
// for minutes to hours and use ProfileLong, which polls less aggressively to
// reduce server load and rate-limit pressure.
//
// This is the canonical source for the polling values; help topics render
// the table from here so documentation stays in sync with behavior.
type WaitProfile string

const (
	// ProfileShort: 1s -> 10s. Used by extract, classify, split, parse, edit,
	// and `runs watch` on those run kinds.
	ProfileShort WaitProfile = "short"
	// ProfileLong: 2s -> 30s. Used by workflow runs (`extend run`) and batch
	// watching (`extend batches watch`).
	ProfileLong WaitProfile = "long"
)

// waitProfileTable holds the per-profile polling parameters. Keep this the
// single source of truth; help-topic rendering reads it.
var waitProfileTable = map[WaitProfile]struct {
	Interval, MaxInterval time.Duration
}{
	ProfileShort: {Interval: 1 * time.Second, MaxInterval: 10 * time.Second},
	ProfileLong:  {Interval: 2 * time.Second, MaxInterval: 30 * time.Second},
}

// WaitProfileOptions returns WaitOptions for the named profile, with the
// caller's timeout applied. Unknown profiles fall back to ProfileShort so
// callers never get a zero-valued (busy-loop) WaitOptions.
func WaitProfileOptions(p WaitProfile, timeout time.Duration) WaitOptions {
	row, ok := waitProfileTable[p]
	if !ok {
		row = waitProfileTable[ProfileShort]
	}
	return WaitOptions{Interval: row.Interval, MaxInterval: row.MaxInterval, Timeout: timeout}
}

// WaitProfileSpec is the public shape of a profile's parameters, exposed for
// documentation rendering.
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
