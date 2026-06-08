package extendx

import extend "github.com/extend-hq/extend-go-sdk"

// Regions maps the short region selector accepted via --region /
// EXTEND_REGION to its base URL. We keep our own map (rather than
// delegating to extend.Environments) so the CLI selector stays compact
// (us|us2|eu) and stable across SDK regenerations that may rename
// environment constants.
var Regions = map[string]string{
	"us":  extend.Environments.Production,    // https://api.extend.ai
	"us2": extend.Environments.ProductionUs2, // https://api.us2.extend.app (legacy; not onboarded)
	"eu":  extend.Environments.ProductionEu1, // https://api.eu1.extend.ai
}

func RegionBaseURL(region string) (string, bool) {
	url, ok := Regions[region]
	return url, ok
}

// KnownRegions are every region the CLI accepts via --region /
// EXTEND_REGION. It includes legacy regions (us2) that are still honored
// for existing deployments but no longer offered to new users; use it for
// validation and error messages, not for presenting choices.
func KnownRegions() []string {
	return []string{"us", "us2", "eu"}
}

// AdvertisedRegions are the regions offered to new users in help text and
// the setup wizard. It is a subset of KnownRegions that omits legacy
// regions (us2) so we don't present an option a new account can't select,
// while KnownRegions still accepts them.
func AdvertisedRegions() []string {
	return []string{"us", "eu"}
}
