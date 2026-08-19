package oauth

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// AuthorizeParams carries everything needed to build the authorization
// request URL for the browser.
type AuthorizeParams struct {
	ClientID    string
	RedirectURI string
	State       string
	// Challenge is the S256 code challenge derived from the verifier.
	Challenge string
	// Resource is the RFC 8707 resource indicator (the API base URL).
	Resource string
}

// AuthorizeURL renders the full authorization endpoint URL the browser
// is sent to.
func AuthorizeURL(authorizationEndpoint string, p AuthorizeParams) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {p.RedirectURI},
		"state":                 {p.State},
		"code_challenge":        {p.Challenge},
		"code_challenge_method": {"S256"},
		"resource":              {p.Resource},
	}
	sep := "?"
	if u, err := url.Parse(authorizationEndpoint); err == nil && u.RawQuery != "" {
		sep = "&"
	}
	return authorizationEndpoint + sep + q.Encode()
}

// callbackResult is what the loopback handler reports back to the flow:
// either an authorization code or the reason the redirect failed.
type callbackResult struct {
	code string
	err  error
}

// Loopback is the ephemeral 127.0.0.1 HTTP listener that receives the
// authorization redirect, per RFC 8252 section 7.3. The port is chosen
// by the OS; the authorization server must accept any port on the
// loopback interface for this client.
type Loopback struct {
	ln     net.Listener
	srv    *http.Server
	result chan callbackResult
}

// NewLoopback binds an ephemeral port on 127.0.0.1 and starts serving
// the callback handler. Callers must Close it when done.
func NewLoopback(state string) (*Loopback, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind loopback listener: %w", err)
	}
	result := make(chan callbackResult, 1)
	srv := &http.Server{
		Handler: newCallbackHandler(state, result),
		// The only legitimate client is the local browser redirect; a
		// header timeout keeps a misbehaving local process from
		// holding connections open for the life of the login.
		ReadHeaderTimeout: 10 * time.Second,
	}
	go srv.Serve(ln) //nolint:errcheck // shut down via Close; Serve's error is always non-nil then
	return &Loopback{ln: ln, srv: srv, result: result}, nil
}

// RedirectURI returns the exact redirect_uri to register on the
// authorization request: http://127.0.0.1:{port}/callback.
func (l *Loopback) RedirectURI() string {
	return fmt.Sprintf("http://%s/callback", l.ln.Addr().String())
}

// Wait blocks until the browser hits the callback (returning the
// authorization code or the redirect's error) or ctx ends.
func (l *Loopback) Wait(ctx context.Context) (string, error) {
	select {
	case r := <-l.result:
		return r.code, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close shuts the listener down. Safe to call after Wait returns.
func (l *Loopback) Close() error {
	return l.srv.Close()
}

// newCallbackHandler builds the handler for the loopback redirect. It
// accepts GET /callback only, verifies the state parameter, and reports
// exactly one result (the first callback wins; later hits get a plain
// error page).
//
// The state check comes first and gates everything, including error
// redirects: any local process can hit this port, and a callback that
// cannot prove it belongs to this login attempt (by echoing the state)
// must not be able to consume the one-shot result channel and abort a
// pending real login. Non-matching callbacks are answered without
// touching the channel.
func newCallbackHandler(state string, result chan<- callbackResult) http.Handler {
	report := func(r callbackResult) bool {
		select {
		case result <- r:
			return true
		default:
			return false
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			writeCallbackPage(w, http.StatusNotFound, "Sign-in failed",
				"This response did not match the current login attempt. You can close this tab and run <code>extend login</code> to try again.")
			return
		}
		if errCode := q.Get("error"); errCode != "" {
			// The error and error_description parameters arrive on a
			// redirect anyone can craft and are printed to the
			// terminal; strip escape sequences before they travel.
			msg := iostreams.SanitizeForTerminal(errCode)
			if desc := q.Get("error_description"); desc != "" {
				msg = msg + ": " + iostreams.SanitizeForTerminal(desc)
			}
			report(callbackResult{err: fmt.Errorf("authorization failed: %s", msg)})
			if errCode == "access_denied" {
				writeCallbackPage(w, http.StatusOK, "Sign-in canceled",
					"No access was granted. You can close this tab and return to your terminal.")
			} else {
				writeCallbackPage(w, http.StatusOK, "Sign-in failed",
					"The authorization was not completed ("+html.EscapeString(msg)+"). You can close this tab and run <code>extend login</code> to try again.")
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			report(callbackResult{err: fmt.Errorf("authorization response missing code parameter")})
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed",
				"The response was missing the authorization code. You can close this tab and run <code>extend login</code> to try again.")
			return
		}
		if !report(callbackResult{code: code}) {
			writeCallbackPage(w, http.StatusConflict, "Already signed in",
				"This login attempt already completed. You can close this tab.")
			return
		}
		writeCallbackPage(w, http.StatusOK, "You're signed in",
			"You can close this tab and return to your terminal.")
	})
	return mux
}

// logomarkSVG is the Extend mark (two stacked hollow blunted diamonds)
// as inline SVG, derived from the same parametric geometry the terminal
// logo uses (internal/cli/logo.go: rx 16, ry 9, corner 1.5, inner
// ratios 0.75/0.67, center gap 6). Inlined so the page renders offline.
const logomarkSVG = `<svg width="44" height="36" viewBox="0 0 40 32" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Extend">
<path fill="#1a1a1a" fill-rule="evenodd" d="M20 4 L36 11.5 L36 14.5 L20 22 L4 14.5 L4 11.5 Z M20 7 L32 13 L20 19 L8 13 Z"/>
<path fill="#1a1a1a" fill-rule="evenodd" d="M20 10 L36 17.5 L36 20.5 L20 28 L4 20.5 L4 17.5 Z M20 13 L32 19 L20 25 L8 19 Z"/>
</svg>`

// writeCallbackPage renders the branded loopback landing page: a white
// card on a neutral background with the Extend logomark, a heading, and
// a short instruction. Everything is inline (embedded CSS, inline SVG,
// system fonts) because the page must render with no network access.
// body is trusted HTML; callers escape any dynamic content they splice
// into it.
func writeCallbackPage(w http.ResponseWriter, status int, heading, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s - Extend</title>
<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:#f4f4f2;color:#1a1a1a;-webkit-font-smoothing:antialiased;
font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif}
main{background:#fff;border:1px solid #e4e4e0;border-radius:16px;
box-shadow:0 1px 2px rgba(0,0,0,.04),0 8px 24px rgba(0,0,0,.06);
padding:56px 48px;margin:24px;max-width:360px;text-align:center}
svg{display:block;margin:0 auto 28px}
h1{font-size:20px;font-weight:600;letter-spacing:-.01em;margin:0 0 10px}
p{font-size:14px;line-height:1.6;color:#52524d;margin:0}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;
background:#f4f4f2;border:1px solid #e4e4e0;border-radius:4px;padding:1px 5px}
</style>
</head>
<body>
<main>
%s
<h1>%s</h1>
<p>%s</p>
</main>
</body>
</html>
`, html.EscapeString(heading), logomarkSVG, html.EscapeString(heading), body)
}
