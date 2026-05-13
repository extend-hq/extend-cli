package cli

import "testing"

// TestPaginationDone codifies the loop-termination rules for the
// shared paginate-page helper. The semantics:
//
//   - End of stream (next == "") always stops.
//   - Single-page mode (--all not set, --max <= 0) stops after the
//     first page, matching historical default behavior.
//   - --max N stops when rowsSoFar >= N, regardless of whether --all
//     was also set.
//   - --all without --max keeps going while pages remain.
func TestPaginationDone(t *testing.T) {
	cases := []struct {
		name      string
		all       bool
		max       int
		rowsSoFar int
		next      string
		wantBreak bool
	}{
		// End of stream wins over every other flag.
		{"end of stream stops single-page", false, 0, 5, "", true},
		{"end of stream stops --all", true, 0, 5, "", true},
		{"end of stream stops --max", false, 100, 5, "", true},
		// Single-page mode (no --all, no --max).
		{"single page after first iter", false, 0, 5, "tok", true},
		// --all without --max keeps going.
		{"--all continues on more pages", true, 0, 5, "tok", false},
		// --max stops when reached.
		{"--max reached", false, 10, 10, "tok", true},
		{"--max not yet reached", false, 10, 5, "tok", false},
		{"--max exceeded (last page overshot)", false, 10, 13, "tok", true},
		// --all + --max: --max bounds the fetch.
		{"--all + --max not reached", true, 100, 50, "tok", false},
		{"--all + --max reached", true, 100, 100, "tok", true},
	}
	for _, tc := range cases {
		got := paginationDone(tc.all, tc.max, tc.rowsSoFar, tc.next)
		if got != tc.wantBreak {
			t.Errorf("%s: paginationDone(all=%v, max=%d, rowsSoFar=%d, next=%q) = %v, want %v",
				tc.name, tc.all, tc.max, tc.rowsSoFar, tc.next, got, tc.wantBreak)
		}
	}
}

// TestCapRowsToMax confirms truncation kicks in only when --max is
// positive and rows exceed it.
func TestCapRowsToMax(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		max  int
		want []int
	}{
		{"unlimited keeps everything", []int{1, 2, 3}, 0, []int{1, 2, 3}},
		{"max larger than slice", []int{1, 2, 3}, 10, []int{1, 2, 3}},
		{"max equal to slice", []int{1, 2, 3}, 3, []int{1, 2, 3}},
		{"max smaller than slice", []int{1, 2, 3, 4, 5}, 3, []int{1, 2, 3}},
		{"max zero", []int{1, 2}, 0, []int{1, 2}},
		{"negative max treated as unlimited", []int{1, 2}, -1, []int{1, 2}},
	}
	for _, tc := range cases {
		got := capRowsToMax(tc.in, tc.max)
		if !equalIntSlice(got, tc.want) {
			t.Errorf("%s: capRowsToMax(%v, %d) = %v, want %v", tc.name, tc.in, tc.max, got, tc.want)
		}
	}
}

func equalIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
