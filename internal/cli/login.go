package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/iostreams"
	"github.com/extend-hq/extend-cli/internal/oauth"
)

// loginWaitTimeout bounds how long `extend login` waits for the browser
// round trip before giving up, so an abandoned attempt does not hold
// the terminal forever.
const loginWaitTimeout = 10 * time.Minute

func newLoginDoc(app *App) *CommandDoc {
	var noBrowser bool
	return &CommandDoc{
		Use:     "login",
		Summary: "Sign in to Extend through your browser (no API key needed)",
		Triggers: []string{
			"sign in to extend from the terminal",
			"log in to the extend cli with my browser",
			"authenticate the cli without creating an api key",
			"oauth login for the extend cli",
		},
		WhenToUse: `Use to authenticate interactively without creating an API key: it opens
your browser, you pick one workspace and one environment on the consent
screen, and the CLI stores the resulting tokens securely. Prefer an API
key (EXTEND_API_KEY or 'extend setup') for CI and unattended use; login
needs a browser once, though the stored session then works headlessly
until it expires or is revoked.`,
		Details: `The flow is a standard native-app OAuth authorization code grant with
PKCE: the CLI listens on an ephemeral 127.0.0.1 port, sends you to the
Extend consent screen, and exchanges the returned code for tokens.

Tokens are stored in the OS keychain when one is available, otherwise in
a 0600 file next to the config file (~/.config/extend/oauth_tokens.json).
Logins are stored per API base URL, so sessions against different
regions or test rigs coexist. Access tokens are short-lived and renewed
silently; when renewal fails you are asked to log in again.

A login is scoped to the one workspace and environment you approve in
the browser, so commands need no --workspace flag under it.

Auth precedence: an API key from the environment (EXTEND_API_KEY) or the
config file always wins over a stored login. Run 'extend config' to see
which source is in effect.`,
		Examples: []Example{
			{Label: "Sign in", Cmd: "extend login"},
			{Label: "Print the URL instead of opening a browser", Cmd: "extend login --no-browser"},
			{Label: "Sign in to the EU region", Cmd: "extend login --region eu"},
		},
		Gotchas: []string{
			"EXTEND_API_KEY and a saved config-file key take precedence over the stored login; unset them if commands seem to ignore your login.",
			"Each login targets exactly one workspace and one environment, chosen on the consent screen; log in again to switch.",
			"Logins are keyed by API base URL: switching --region or EXTEND_BASE_URL selects a different stored session (or none).",
			"With --env <label> set, commands read EXTEND_<LABEL>_API_KEY only and never fall back to the stored login.",
			"Logins to the Extend regions need no configuration; a custom EXTEND_BASE_URL (staging, rigs) needs EXTEND_OAUTH_ISSUER and usually EXTEND_OAUTH_CLIENT_ID.",
		},
		SeeAlso: []string{"logout", "whoami", "setup", "auth"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the sign-in URL instead of opening a browser")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), app, loginOptions{
				noBrowser:   noBrowser,
				openBrowser: openBrowser,
				store:       oauth.DefaultStore(),
			})
		},
	}
}

func newLogoutDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "logout",
		Summary: "Sign out: revoke the stored login and clear its tokens",
		Triggers: []string{
			"log out of the extend cli",
			"sign out and revoke my extend session",
			"clear stored oauth tokens for extend",
		},
		WhenToUse: `Use to end a session created by 'extend login': it revokes the grant
server-side and deletes the locally stored tokens. It does not touch API
keys (environment variables or the config file); delete those separately
if needed.`,
		Examples: []Example{
			{Label: "Sign out", Cmd: "extend logout"},
			{Label: "Sign out of the EU region", Cmd: "extend logout --region eu"},
		},
		Gotchas: []string{
			"Logout only affects the login for the currently selected API base URL (region or EXTEND_BASE_URL); other regions' sessions remain.",
			"Running logout while not logged in is fine; it reports there was nothing to do and exits 0.",
		},
		SeeAlso: []string{"login", "whoami", "auth"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), app, oauth.DefaultStore())
		},
	}
}

func newWhoamiDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "whoami",
		Summary: "Show the workspace, environment, and user of the current credentials",
		Triggers: []string{
			"which extend account am i using",
			"check who the extend cli is signed in as",
			"show current extend workspace and auth method",
		},
		WhenToUse: `Use to confirm which workspace and environment your commands will hit
and which credential supplies them (API key or a stored 'extend login'),
before running anything that writes.`,
		Examples: []Example{
			{Label: "Show the current identity", Cmd: "extend whoami"},
		},
		SeeAlso: []string{"login", "logout", "config", "auth"},
		Output:  OutputSpec{TTY: OutputPretty, Pipe: OutputPretty},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami(cmd.Context(), app)
		},
	}
}

func runWhoami(ctx context.Context, app *App) error {
	s := resolveSettings(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	base, err := effectiveBaseURL(s)
	if err != nil {
		return err
	}

	bearer := s.key.val
	method := "API key (" + s.key.src + ")"
	if bearer == "" {
		src, srcErr := resolveOAuthSource(app.Env, s, app.IO.ErrOut)
		if src == nil {
			return unconfiguredKeyError(apiKeyEnvVar(app.Env), s.region.val, s.fileErr, srcErr)
		}
		if bearer, err = src.AccessToken(ctx); err != nil {
			return err
		}
		method = "OAuth login"
	}

	id, err := fetchIdentity(ctx, oauth.NewHTTPClient(base), base, bearer)
	if err != nil {
		return fmt.Errorf("fetch identity: %w", err)
	}
	if ws := workspaceLabel(id); ws != "" {
		fmt.Fprintf(app.IO.Out, "Workspace  %s\n", ws)
	}
	if id.email != "" {
		fmt.Fprintf(app.IO.Out, "User       %s\n", id.email)
	}
	fmt.Fprintf(app.IO.Out, "Auth       %s\n", method)
	fmt.Fprintf(app.IO.Out, "Base URL   %s\n", base)
	return nil
}

// loginOptions carries the injectable pieces of runLogin so tests can
// substitute a fake browser and an isolated store.
type loginOptions struct {
	noBrowser   bool
	openBrowser func(url string) error
	store       oauth.Store
}

func runLogin(ctx context.Context, app *App, opts loginOptions) error {
	s := resolveSettings(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	base, err := effectiveBaseURL(s)
	if err != nil {
		return err
	}
	pal := paletteFor(app.IO)

	// The authorization server is the WorkOS AuthKit issuer, not the API:
	// discovery, the browser authorize URL, and the token endpoint all
	// live on the issuer domain, while the RFC 8707 resource parameter
	// and every bearer-authenticated API call point at the API base.
	issuer, clientID, err := resolveAuthServer(os.Getenv, base)
	if err != nil {
		return err
	}

	// Two pinned clients, one per origin: OAuth calls (discovery, code
	// exchange, refresh) may only talk to the issuer, API calls (/me)
	// only to the API base, so request bodies carrying the code,
	// verifier, or tokens can never be replayed to another host.
	authHTTP := oauth.NewHTTPClient(issuer)
	httpc := oauth.NewHTTPClient(base)
	eps, err := oauth.Discover(ctx, authHTTP, issuer)
	if err != nil {
		return err
	}

	verifier, err := oauth.NewVerifier()
	if err != nil {
		return err
	}
	state, err := oauth.NewState()
	if err != nil {
		return err
	}

	lb, err := oauth.NewLoopback(state)
	if err != nil {
		return err
	}
	defer lb.Close()

	authURL := oauth.AuthorizeURL(eps.Authorization, oauth.AuthorizeParams{
		ClientID:    clientID,
		RedirectURI: lb.RedirectURI(),
		State:       state,
		Challenge:   oauth.ChallengeS256(verifier),
		Resource:    base,
	})

	if opts.noBrowser {
		fmt.Fprintf(app.IO.ErrOut, "Open this URL in your browser to sign in:\n\n    %s\n\n", authURL)
	} else if openErr := opts.openBrowser(authURL); openErr != nil {
		fmt.Fprintf(app.IO.ErrOut, "%s Could not open a browser (%v).\n", pal.Yellow("!"), openErr)
		fmt.Fprintf(app.IO.ErrOut, "Open this URL to sign in:\n\n    %s\n\n", authURL)
	} else {
		fmt.Fprintf(app.IO.ErrOut, "Opening your browser to sign in. If nothing opens, use this URL:\n\n    %s\n\n", authURL)
	}

	waitCtx, cancel := context.WithTimeout(ctx, loginWaitTimeout)
	defer cancel()
	sp := app.IO.StartSpinner("Waiting for you to approve access in the browser...")
	code, err := lb.Wait(waitCtx)
	if err != nil {
		sp.Stop("")
		if waitCtx.Err() != nil && ctx.Err() == nil {
			return fmt.Errorf("timed out waiting for the browser sign-in; run 'extend login' to try again")
		}
		return err
	}

	sp.Update("Completing sign-in...")
	tokenClient := &oauth.Client{
		HTTPClient: authHTTP,
		Endpoints:  eps,
		ClientID:   clientID,
		Resource:   base,
	}
	tr, err := tokenClient.Exchange(ctx, code, verifier, lb.RedirectURI())
	if err != nil {
		sp.Stop("")
		return fmt.Errorf("exchange authorization code: %w", err)
	}

	rec := oauth.Record{
		AccessToken:   tr.AccessToken,
		RefreshToken:  tr.RefreshToken,
		ExpiresAt:     tr.Expiry(time.Now()),
		TokenEndpoint: eps.Token,
		// No RevocationEndpoint: WorkOS Connect has no RFC 7009
		// endpoint; logout revokes via the API's /oauth/revoke-current.
		ClientID: clientID,
		Resource: base,
	}
	// The replaced record (if any) is simply overwritten, never revoked:
	// /oauth/revoke-current deletes the CLI client's whole WorkOS
	// authorization, which the new login shares, so revoking the old
	// grant would cut off the new session's refresh ability too. The
	// superseded tokens leave the keychain here and the authorization
	// itself dies at logout.
	if err := opts.store.Set(base, rec); err != nil {
		sp.Stop("")
		return fmt.Errorf("store login: %w", err)
	}

	// Personalize the success line from GET /me. Best-effort: the
	// tokens are already stored, so a /me hiccup must never fail the
	// login; the generic line is the fallback.
	id, _ := fetchIdentity(ctx, httpc, base, tr.AccessToken)
	sp.Stop("")
	fmt.Fprintf(app.IO.ErrOut, "%s %s\n", pal.Green("✓"), loginSuccessLine(base, id))
	if s.key.val != "" {
		fmt.Fprintf(app.IO.ErrOut, "%s An API key is also configured (%s) and takes precedence over this login.\n",
			pal.Yellow("!"), s.key.src)
	}
	return nil
}

func runLogout(ctx context.Context, app *App, store oauth.Store) error {
	s := resolveSettings(app.Env, app.Region, app.Workspace, os.Getenv, config.Load)
	base, err := effectiveBaseURL(s)
	if err != nil {
		return err
	}
	pal := paletteFor(app.IO)

	rec, err := store.Get(base)
	if err != nil {
		return fmt.Errorf("read stored login: %w", err)
	}
	if rec == nil {
		fmt.Fprintf(app.IO.ErrOut, "Not logged in to %s; nothing to do.\n", base)
		return nil
	}

	// Best-effort: local tokens are cleared even when the server-side
	// revoke fails (the API returns a retryable 503 when it cannot reach
	// WorkOS), so the CLI is signed out either way; the user can finish
	// the revocation from their account settings.
	if revErr := revokeRecord(ctx, base, rec); revErr != nil {
		fmt.Fprintf(app.IO.ErrOut, "%s Could not revoke the session server-side (%v); clearing local tokens anyway.\n",
			pal.Yellow("!"), revErr)
		fmt.Fprintf(app.IO.ErrOut, "%s The CLI may still appear under your connected apps; disconnect it from your Extend user settings if needed.\n",
			pal.Yellow("!"))
	}

	if err := store.Delete(base); err != nil {
		return fmt.Errorf("clear stored login: %w", err)
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Logged out of %s.\n", pal.Green("✓"), base)
	return nil
}

// revokeRecord kills a stored login server-side. WorkOS Connect has no
// RFC 7009 revocation endpoint; instead POST /oauth/revoke-current on
// the API deletes the CLI client's WorkOS authorization, which stops
// new tokens from being minted (refresh tokens die with it). Already
// issued access tokens stay valid until they expire — up to an hour —
// but this CLI discards its copy right after. An expired access token
// is refreshed first so the revoke call can authenticate.
func revokeRecord(ctx context.Context, base string, rec *oauth.Record) error {
	if rec == nil || rec.AccessToken == "" {
		return nil
	}
	token := rec.AccessToken
	stale := !rec.ExpiresAt.IsZero() && time.Now().After(rec.ExpiresAt.Add(-30*time.Second))
	if stale && rec.RefreshToken != "" && rec.TokenEndpoint != "" {
		clientID := rec.ClientID
		if clientID == "" {
			clientID = oauth.DefaultClientID
		}
		c := &oauth.Client{
			HTTPClient: oauth.NewHTTPClient(rec.TokenEndpoint),
			Endpoints:  oauth.Endpoints{Token: rec.TokenEndpoint},
			ClientID:   clientID,
			Resource:   base,
		}
		if tr, err := c.Refresh(ctx, rec.RefreshToken); err == nil {
			token = tr.AccessToken
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		oauth.NormalizeBase(base)+"/oauth/revoke-current", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent())
	resp, err := oauth.NewHTTPClient(base).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST /oauth/revoke-current: http %d", resp.StatusCode)
	}
	return nil
}

// resolveAuthServer returns the authorization-server issuer (the WorkOS
// AuthKit domain) and OAuth client id for 'extend login'. Each region
// carries a built-in issuer and CLI client id, matched by the resolved
// API base URL, so logins to the regional deployments need no
// configuration. EXTEND_OAUTH_ISSUER overrides the issuer — and disables
// the baked client id with it, because client ids are per-issuer —
// while EXTEND_OAUTH_CLIENT_ID overrides the client id alone. Non-region
// bases (staging, rigs) have no built-in issuer and fail with
// instructions instead.
func resolveAuthServer(getenv func(string) string, base string) (issuer, clientID string, err error) {
	issuer = oauth.NormalizeBase(getenv(envOAuthIssuer))
	clientID = getenv(envOAuthClientID)
	if issuer == "" {
		if r, ok := regionForBase(base); ok && r.LoginIssuer != "" {
			issuer = oauth.NormalizeBase(r.LoginIssuer)
			if clientID == "" {
				clientID = r.LoginClientID
			}
		}
	}
	if issuer == "" {
		return "", "", fmt.Errorf("no sign-in domain is known for %s; set %s to this environment's sign-in domain (e.g. https://id.extend.ai) to use 'extend login'", base, envOAuthIssuer)
	}
	if err := oauth.ValidateBaseURL(issuer); err != nil {
		return "", "", err
	}
	if clientID == "" {
		clientID = oauth.DefaultClientID
	}
	return issuer, clientID, nil
}

// regionForBase returns the region whose API base URL matches the
// resolved base, if any. base must already be normalized.
func regionForBase(base string) (extendx.Region, bool) {
	for _, r := range extendx.Regions() {
		if oauth.NormalizeBase(r.APIURL) == base {
			return r, true
		}
	}
	return extendx.Region{}, false
}

// effectiveBaseURL resolves the API base URL the CLI is pointed at:
// EXTEND_BASE_URL / config-file baseUrl wins, then the region (flag,
// env, or config file), then the default US production URL. The login
// flow, discovery, resource parameter, and token storage all key off
// this value.
func effectiveBaseURL(s resolved) (string, error) {
	base := ""
	if s.baseURL.val != "" {
		base = oauth.NormalizeBase(s.baseURL.val)
	} else {
		region := s.region.val
		if region == "" {
			region = "us"
		}
		u, ok := extendx.RegionBaseURL(region)
		if !ok {
			return "", fmt.Errorf("unknown region %q (known: %v)", region, extendx.KnownRegions())
		}
		base = oauth.NormalizeBase(u)
	}
	if err := oauth.ValidateBaseURL(base); err != nil {
		return "", err
	}
	return base, nil
}

// loginIdentity is the slice of GET /me the success line needs: the
// workspace and environment this login was granted, and who signed in.
type loginIdentity struct {
	workspace   string
	environment string
	email       string
}

// fetchIdentity calls GET /me with a bearer credential (an OAuth
// access token or an API key; the endpoint accepts both). The SDK has
// no /me binding yet, so this is a minimal direct call.
func fetchIdentity(ctx context.Context, httpc *http.Client, base, bearer string) (*loginIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("x-extend-api-version", defaultAPIVersion)
	req.Header.Set("User-Agent", userAgent())
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /me: http %d", resp.StatusCode)
	}
	var body struct {
		Workspace struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspace"`
		User struct {
			Email string `json:"email"`
		} `json:"user"`
		GrantedTargets []struct {
			Workspace struct {
				ID string `json:"id"`
			} `json:"workspace"`
			Environments []string `json:"environments"`
		} `json:"grantedTargets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("parse /me response: %w", err)
	}
	id := &loginIdentity{
		workspace: iostreams.SanitizeForTerminal(body.Workspace.Name),
		email:     iostreams.SanitizeForTerminal(body.User.Email),
	}
	// The top-level workspace is the one this login resolved to; its
	// granted target carries the environment. A login is scoped to one
	// environment, so anything but a single entry stays unlabeled.
	for _, t := range body.GrantedTargets {
		if t.Workspace.ID == body.Workspace.ID && len(t.Environments) == 1 {
			id.environment = t.Environments[0]
			break
		}
	}
	return id, nil
}

// workspaceLabel renders "Acme (Production)" from an identity, or ""
// when /me gave no workspace. The parenthetical is the workspace's
// granted environment, not the deployment.
func workspaceLabel(id *loginIdentity) string {
	if id == nil || id.workspace == "" {
		return ""
	}
	switch id.environment {
	case "PRODUCTION":
		return id.workspace + " (Production)"
	case "TEST":
		return id.workspace + " (Test)"
	}
	return id.workspace
}

// loginSuccessLine renders the post-login summary: personalized when
// /me answered ("Signed in to Acme (Production) as a@b.c."), generic
// otherwise. Non-region bases (staging, rigs) are named in the
// personalized line too — the environment parenthetical would
// otherwise read as the production deployment.
func loginSuccessLine(base string, id *loginIdentity) string {
	ws := workspaceLabel(id)
	if ws == "" {
		return fmt.Sprintf("Signed in to %s.", base)
	}
	line := "Signed in to " + ws
	if id.email != "" {
		line += " as " + id.email
	}
	if !isKnownRegionBase(base) {
		line += " on " + base
	}
	return line + "."
}

// isKnownRegionBase reports whether base is one of the known regions'
// API URLs (as opposed to a custom EXTEND_BASE_URL target).
func isKnownRegionBase(base string) bool {
	_, ok := regionForBase(base)
	return ok
}

// openBrowser launches the platform's URL opener. Errors are surfaced
// to the caller, which falls back to printing the URL.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
