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

func TestAdvertisedRegionsAreKnownAndComplete(t *testing.T) {
	known := map[string]bool{}
	for _, id := range KnownRegions() {
		known[id] = true
	}
	for _, r := range AdvertisedRegions() {
		if !known[r.ID] {
			t.Errorf("advertised region %q is not in KnownRegions", r.ID)
		}
		if r.APIURL == "" || r.Title == "" || r.Dashboard == "" {
			t.Errorf("advertised region %q is missing display metadata: %+v", r.ID, r)
		}
	}
	// us2 is accepted but must not be advertised to new users.
	for _, r := range AdvertisedRegions() {
		if r.ID == "us2" {
			t.Error("us2 is deprecated and must not be advertised")
		}
	}
	if _, ok := RegionBaseURL("us2"); !ok {
		t.Error("us2 must still resolve for existing customers")
	}
}
