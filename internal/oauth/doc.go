// Package oauth implements the CLI side of Extend's first-party OAuth
// login: an RFC 8252 native-app authorization code flow with PKCE (S256)
// against the Extend API's own authorization endpoints, plus storage and
// silent refresh of the resulting tokens.
//
// The wire contract (fixed so the CLI and API sides can build in
// parallel):
//
//   - Authorization server endpoints live on the API base URL:
//     {apiBase}/oauth2/authorize, /oauth2/token, /oauth2/revoke.
//     RFC 8414 discovery via {apiBase}/.well-known/oauth-authorization-server
//     is preferred, with the hardcoded paths as fallback.
//   - The client is the static public client "extend-cli" (no secret;
//     PKCE required). EXTEND_OAUTH_CLIENT_ID overrides it for test rigs.
//   - The resource parameter (RFC 8707) is required on both the
//     authorize and token requests and equals the API base URL the CLI
//     is configured against.
//   - Access tokens are opaque, prefixed "eoat_", with a short TTL
//     (time to live). Refresh tokens are rotating, prefixed "eort_";
//     every refresh returns a new one that must be persisted.
//   - Each grant targets exactly one workspace and one environment,
//     chosen in the browser consent wizard, so API calls carry only the
//     bearer token and the server infers the rest.
package oauth
