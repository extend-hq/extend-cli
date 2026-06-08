package extendx

import extend "github.com/extend-hq/extend-go-sdk"

// Region is the metadata for one API region. Advertised regions are
// offered to new users (help text, the setup wizard); non-advertised ones
// (e.g. us2) are still accepted via --region / EXTEND_REGION for existing
// deployments but no longer onboarded. Title/Dashboard are presentation
// fields the wizard renders; keeping them here makes this the single
// source of truth so adding a region is a one-line change.
type Region struct {
	ID         string
	Title      string
	APIURL     string
	Dashboard  string
	Advertised bool
}

// regions is the single source of truth; every accessor derives from it.
var regions = []Region{
	{ID: "us", Title: "United States", APIURL: extend.Environments.Production, Dashboard: "https://dashboard.extend.ai", Advertised: true},
	{ID: "us2", Title: "United States (US2)", APIURL: extend.Environments.ProductionUs2, Dashboard: "https://dashboard.us2.extend.app", Advertised: false},
	{ID: "eu", Title: "European Union", APIURL: extend.Environments.ProductionEu1, Dashboard: "https://dashboard.eu1.extend.ai", Advertised: true},
}

// RegionBaseURL returns the API base URL for a region id.
func RegionBaseURL(id string) (string, bool) {
	for _, r := range regions {
		if r.ID == id {
			return r.APIURL, true
		}
	}
	return "", false
}

// RegionDashboard returns the dashboard URL for a region id (advertised or
// legacy), used to point users at the right place to create an API key.
func RegionDashboard(id string) (string, bool) {
	for _, r := range regions {
		if r.ID == id {
			return r.Dashboard, true
		}
	}
	return "", false
}

// KnownRegions lists every accepted region id (advertised or not). Use it
// for validation and error messages, not for presenting choices.
func KnownRegions() []string {
	ids := make([]string, len(regions))
	for i, r := range regions {
		ids[i] = r.ID
	}
	return ids
}

// AdvertisedRegions returns the regions offered to new users, in display
// order. A subset of KnownRegions that omits legacy regions.
func AdvertisedRegions() []Region {
	var out []Region
	for _, r := range regions {
		if r.Advertised {
			out = append(out, r)
		}
	}
	return out
}
