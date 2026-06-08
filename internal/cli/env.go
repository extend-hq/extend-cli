package cli

// envVarSpec describes an environment variable consulted by the CLI. The
// list is the canonical source for documentation; auth wiring reads names
// from here too so the documented contract and runtime behavior cannot
// drift apart.
type envVarSpec struct {
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
	envAPIKey        = "EXTEND_API_KEY"
	envBaseURL       = "EXTEND_BASE_URL"
	envRegion        = "EXTEND_REGION"
	envWorkspaceID   = "EXTEND_WORKSPACE_ID"
	envAPIVersion    = "EXTEND_API_VERSION"
	envHTTPTimeout   = "EXTEND_HTTP_TIMEOUT"
	envWebhookSecret = "EXTEND_WEBHOOK_SECRET"
	envDebug         = "EXTEND_DEBUG"
	envOutput        = "EXTEND_OUTPUT"
	envEnv           = "EXTEND_ENV"
	// envSkipSkillInstall, when truthy, makes non-interactive `extend
	// setup` skip installing the agent skill. Install scripts forward it so
	// their --skip-skill-install knob still works after delegating to the
	// CLI. Not in envVars: it's a setup-time knob, not an auth/routing var.
	envSkipSkillInstall = "EXTEND_SKIP_SKILL_INSTALL"
)

// defaultAPIVersion is the API version sent on every request unless the
// caller overrides via EXTEND_API_VERSION. Mirrors the SDK's hard-coded
// default; we surface it here so the auth help topic can render it.
const defaultAPIVersion = "2026-02-09"

// envVars enumerates every environment variable the CLI consults. Order is
// stable and matches the priority a user is most likely to care about
// (auth first, then routing, then transport, then per-feature secrets).
var envVars = []envVarSpec{
	{Name: envAPIKey, Required: true, Description: "API key (sk_...). Required for any command that calls the API."},
	{Name: envBaseURL, Description: "Override base URL. Wins over EXTEND_REGION."},
	{Name: envRegion, Description: "Region: us|eu. Selects the regional API endpoint."},
	{Name: envWorkspaceID, Description: "Workspace ID for org-scoped API keys (sent as X-Extend-Workspace-Id)."},
	{Name: envAPIVersion, Description: "Pin the API version sent with each request. Defaults to " + defaultAPIVersion + "."},
	{Name: envHTTPTimeout, Description: "Per-HTTP-request timeout (e.g. 60s, 2m). Applies to each API call individually; uploads use a separate untimed client. Distinct from per-command --timeout (overall wait). Defaults to 60s."},
	{Name: envWebhookSecret, Description: "Default signing secret used by 'extend webhooks verify'."},
	{Name: envDebug, Description: "Set to 1 to log every HTTP request to stderr (method, URL, status, request ID, latency, error bodies)."},
	{Name: envOutput, Description: "Default output format when --output is not set: json|yaml|raw|id|table|markdown."},
	{Name: envEnv, Description: "Environment label (e.g. 'test') that selects EXTEND_<UPPER>_API_KEY in place of EXTEND_API_KEY."},
}
