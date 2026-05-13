package cli

// pagination flag helpers shared by every list command. Page tokens are
// an API implementation detail; the primary user-facing knob is --max
// (auto-paginate up to N total results). --all stays as the explicit
// "fetch everything" shortcut; --page-token stays for callers that
// want explicit cursor control.

// paginationDone reports whether the page-iteration loop should stop
// after the most recent page was appended.
//
// The loop continues while at least one of these is true and there's a
// next page:
//   - --all is set (fetch every page)
//   - --max N is set and the accumulator hasn't reached N yet
//
// When neither flag is set, the loop runs exactly once (single-page
// mode), matching the historical default.
//
// Truncation to exactly N happens after the loop via capRowsToMax.
func paginationDone(all bool, max, rowsSoFar int, next string) bool {
	if next == "" {
		return true
	}
	if !all && max <= 0 {
		return true
	}
	if max > 0 && rowsSoFar >= max {
		return true
	}
	return false
}

// capRowsToMax trims a row slice to at most max entries. No-op when
// max <= 0 (unlimited).
func capRowsToMax[T any](rows []T, max int) []T {
	if max > 0 && len(rows) > max {
		return rows[:max]
	}
	return rows
}
