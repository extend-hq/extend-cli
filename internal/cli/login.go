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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/config"
	"github.com/extend-hq/extend-cli/internal/extendx"
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
		},
		SeeAlso: []string{"logout", "setup", "auth"},
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
		SeeAlso: []string{"login", "auth"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), app, oauth.DefaultStore())
		},
	}
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

	httpc := &http.Client{Timeout: 30 * time.Second}
	eps, err := oauth.Discover(ctx, httpc, base)
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
		ClientID:    oauth.DefaultClientID,
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
		HTTPClient: httpc,
		Endpoints:  eps,
		ClientID:   oauth.DefaultClientID,
		Resource:   base,
	}
	tr, err := tokenClient.Exchange(ctx, code, verifier, lb.RedirectURI())
	if err != nil {
		sp.Stop("")
		return fmt.Errorf("exchange authorization code: %w", err)
	}

	rec := oauth.Record{
		AccessToken:        tr.AccessToken,
		RefreshToken:       tr.RefreshToken,
		ExpiresAt:          time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		TokenEndpoint:      eps.Token,
		RevocationEndpoint: eps.Revocation,
		ClientID:           oauth.DefaultClientID,
		Resource:           base,
	}
	if err := opts.store.Set(base, rec); err != nil {
		sp.Stop("")
		return fmt.Errorf("store login: %w", err)
	}

	// Personalize the success line from GET /me. Best-effort: the
	// tokens are already stored, so a /me hiccup must never fail the
	// login; the generic line is the fallback.
	id := fetchLoginIdentity(ctx, httpc, base, tr.AccessToken)
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

	// Revoking the refresh token kills the whole grant family
	// server-side. Best-effort: local tokens are cleared even when the
	// server is unreachable, so the CLI is signed out either way.
	if rec.RefreshToken != "" {
		eps := oauth.DefaultEndpoints(base)
		if rec.RevocationEndpoint != "" {
			eps.Revocation = rec.RevocationEndpoint
		}
		clientID := rec.ClientID
		if clientID == "" {
			clientID = oauth.DefaultClientID
		}
		revokeClient := &oauth.Client{
			HTTPClient: &http.Client{Timeout: 30 * time.Second},
			Endpoints:  eps,
			ClientID:   clientID,
			Resource:   base,
		}
		if revErr := revokeClient.Revoke(ctx, rec.RefreshToken); revErr != nil {
			fmt.Fprintf(app.IO.ErrOut, "%s Could not revoke the session server-side (%v); clearing local tokens anyway.\n",
				pal.Yellow("!"), revErr)
		}
	}

	if err := store.Delete(base); err != nil {
		return fmt.Errorf("clear stored login: %w", err)
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Logged out of %s.\n", pal.Green("✓"), base)
	return nil
}

// effectiveBaseURL resolves the API base URL the CLI is pointed at:
// EXTEND_BASE_URL / config-file baseUrl wins, then the region (flag,
// env, or config file), then the default US production URL. The login
// flow, discovery, resource parameter, and token storage all key off
// this value.
func effectiveBaseURL(s resolved) (string, error) {
	if s.baseURL.val != "" {
		return oauth.NormalizeBase(s.baseURL.val), nil
	}
	region := s.region.val
	if region == "" {
		region = "us"
	}
	u, ok := extendx.RegionBaseURL(region)
	if !ok {
		return "", fmt.Errorf("unknown region %q (known: %v)", region, extendx.KnownRegions())
	}
	return oauth.NormalizeBase(u), nil
}

// loginIdentity is the slice of GET /me the success line needs: the
// workspace and environment this login was granted, and who signed in.
type loginIdentity struct {
	workspace   string
	environment string
	email       string
}

// fetchLoginIdentity calls GET /me with the fresh access token. The
// SDK has no /me binding yet, so this is a minimal direct call. Any
// failure returns nil; the caller falls back to the generic line.
func fetchLoginIdentity(ctx context.Context, httpc *http.Client, base, accessToken string) *loginIdentity {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/me", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("x-extend-api-version", defaultAPIVersion)
	req.Header.Set("User-Agent", userAgent())
	resp, err := httpc.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
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
		return nil
	}
	id := &loginIdentity{
		workspace: sanitizeForTerminal(body.Workspace.Name),
		email:     sanitizeForTerminal(body.User.Email),
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
	return id
}

// loginSuccessLine renders the post-login summary: personalized when
// /me answered ("Signed in to Acme (Production) as a@b.c."), generic
// otherwise.
func loginSuccessLine(base string, id *loginIdentity) string {
	if id == nil || id.workspace == "" {
		return fmt.Sprintf("Logged in to %s.", base)
	}
	line := "Signed in to " + id.workspace
	switch id.environment {
	case "PRODUCTION":
		line += " (Production)"
	case "TEST":
		line += " (Test)"
	}
	if id.email != "" {
		line += " as " + id.email
	}
	return line + "."
}

// sanitizeForTerminal strips escape sequences and control characters
// from server-provided text before it is printed. A workspace name (or
// email) is attacker-influenced data; without this, embedded ANSI CSI
// or OSC sequences could restyle the terminal, spoof output, or (on
// some emulators) worse. ESC-initiated CSI and OSC sequences are
// removed whole, including their payload; all other C0 controls, DEL,
// and the raw C1 range (which includes single-byte CSI) are dropped.
// These are single-line values, so newlines carry no meaning here and
// are dropped with the rest.
func sanitizeForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b { // ESC
			if i+1 >= len(runes) {
				break
			}
			switch runes[i+1] {
			case '[': // CSI: parameters end at a byte in 0x40–0x7e
				i++
				for i+1 < len(runes) {
					i++
					if runes[i] >= 0x40 && runes[i] <= 0x7e {
						break
					}
				}
			case ']': // OSC: terminated by BEL or ST (ESC \)
				i++
				for i+1 < len(runes) {
					i++
					if runes[i] == 0x07 {
						break
					}
					if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
						i++
						break
					}
				}
			default: // two-character escape (RIS, charset shifts, ...)
				i++
			}
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
