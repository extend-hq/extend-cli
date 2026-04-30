package client

// EnvVarSpec describes an environment variable consulted by the CLI. The
// list is the canonical source for documentation; auth wiring reads names
// from here too so the documented contract and runtime behavior cannot
// drift apart.
type EnvVarSpec struct {
	// Name is the env var name (e.g. "EXTEND_API_KEY").
	Name string
	// Required signals whether the CLI cannot make API requests without it.
	// "Required" means required for any command that actually hits the API;
	// `extend version`, `extend webhooks verify`, and the help topics work
	// without it.
	Required bool
	// Description is a one-line user-facing explanation suitable for help
	// rendering. Keep it short; longer guidance belongs in topic prose.
	Description string
}

// Env var names. Use these constants in code that reads env vars so
// renaming or auditing is a single edit.
const (
	EnvAPIKey        = "EXTEND_API_KEY"
	EnvBaseURL       = "EXTEND_BASE_URL"
	EnvRegion        = "EXTEND_REGION"
	EnvWorkspaceID   = "EXTEND_WORKSPACE_ID"
	EnvAPIVersion    = "EXTEND_API_VERSION"
	EnvWebhookSecret = "EXTEND_WEBHOOK_SECRET"
)

// EnvVars enumerates every environment variable the CLI consults. Order is
// stable and matches the priority a user is most likely to care about
// (auth first, then routing, then transport, then per-feature secrets).
var EnvVars = []EnvVarSpec{
	{Name: EnvAPIKey, Required: true, Description: "API key (sk_...). Required for any command that calls the API."},
	{Name: EnvBaseURL, Description: "Override base URL. Wins over EXTEND_REGION."},
	{Name: EnvRegion, Description: "Region: us|us2|eu. Selects the regional API endpoint."},
	{Name: EnvWorkspaceID, Description: "Workspace ID for org-scoped API keys (sent as X-Extend-Workspace-Id)."},
	{Name: EnvAPIVersion, Description: "Pin the API version sent with each request. Defaults to " + DefaultAPIVersion + "."},
	{Name: EnvWebhookSecret, Description: "Default signing secret used by 'extend webhooks verify'."},
}
