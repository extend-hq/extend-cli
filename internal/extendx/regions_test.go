package extendx

import (
	"sort"
	"strings"
	"testing"
)

func TestRegionBaseURL(t *testing.T) {
	for _, region := range KnownRegions() {
		url, ok := RegionBaseURL(region)
		if !ok {
			t.Errorf("RegionBaseURL(%q) = (_, false); want true", region)
		}
		if !strings.HasPrefix(url, "https://") {
			t.Errorf("RegionBaseURL(%q) = %q; expected https:// URL", region, url)
		}
	}
}

func TestRegionBaseURL_Unknown(t *testing.T) {
	if _, ok := RegionBaseURL("xx"); ok {
		t.Error("RegionBaseURL(xx) = (_, true); want false")
	}
	if _, ok := RegionBaseURL(""); ok {
		t.Error("RegionBaseURL(\"\") = (_, true); want false")
	}
}

func TestKnownRegions(t *testing.T) {
	got := KnownRegions()
	want := []string{"eu", "us", "us2"}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("KnownRegions() length = %d; want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("KnownRegions()[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestRegionsMapIsExhaustive(t *testing.T) {
	// Every region returned by KnownRegions must resolve via the
	// Regions map. This catches drift between the two when a new
	// region is added — KnownRegions is a manually-maintained list.
	for _, r := range KnownRegions() {
		if _, ok := Regions[r]; !ok {
			t.Errorf("KnownRegions returned %q but Regions map has no entry", r)
		}
	}
	// And vice versa: every map key must appear in KnownRegions.
	known := map[string]bool{}
	for _, r := range KnownRegions() {
		known[r] = true
	}
	for r := range Regions {
		if !known[r] {
			t.Errorf("Regions has key %q but KnownRegions does not list it", r)
		}
	}
}
