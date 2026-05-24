package extendx

import extend "github.com/extend-hq/extend-go-sdk"

// Regions maps the short region selector accepted via --region /
// EXTEND_REGION to its base URL. We keep our own map (rather than
// delegating to extend.Environments) so the CLI selector stays compact
// (us|us2|eu) and stable across SDK regenerations that may rename
// environment constants.
var Regions = map[string]string{
	"us":  extend.Environments.Production,    // https://api.extend.ai
	"us2": extend.Environments.ProductionUs2, // https://api.us2.extend.app
	"eu":  extend.Environments.ProductionEu1, // https://api.eu1.extend.ai
}

func RegionBaseURL(region string) (string, bool) {
	url, ok := Regions[region]
	return url, ok
}

func KnownRegions() []string {
	return []string{"us", "us2", "eu"}
}
