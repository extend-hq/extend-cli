package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	srv := &http.Server{Handler: newCallbackHandler(state, result)}
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
		q := r.URL.Query()
		if errCode := q.Get("error"); errCode != "" {
			msg := errCode
			if desc := q.Get("error_description"); desc != "" {
				msg = errCode + ": " + desc
			}
			report(callbackResult{err: fmt.Errorf("authorization failed: %s", msg)})
			writeCallbackPage(w, http.StatusOK, "Sign-in failed",
				"The authorization was not completed ("+msg+"). You can close this tab and return to the terminal.")
			return
		}
		if q.Get("state") != state {
			report(callbackResult{err: fmt.Errorf("authorization response state mismatch; possible CSRF, aborting login")})
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed",
				"The response did not match this login attempt. Close this tab and run 'extend login' again.")
			return
		}
		code := q.Get("code")
		if code == "" {
			report(callbackResult{err: fmt.Errorf("authorization response missing code parameter")})
			writeCallbackPage(w, http.StatusBadRequest, "Sign-in failed",
				"The response was missing the authorization code. Close this tab and run 'extend login' again.")
			return
		}
		if !report(callbackResult{code: code}) {
			writeCallbackPage(w, http.StatusConflict, "Already signed in",
				"This login attempt already completed. You can close this tab.")
			return
		}
		writeCallbackPage(w, http.StatusOK, "Signed in to Extend",
			"Login complete. You can close this tab and return to the terminal.")
	})
	return mux
}

func writeCallbackPage(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html>
<html><head><title>%s</title></head>
<body style="font-family: -apple-system, system-ui, sans-serif; margin: 4rem auto; max-width: 32rem; text-align: center;">
<h1 style="font-size: 1.25rem;">%s</h1>
<p style="color: #555;">%s</p>
</body></html>
`, title, title, body)
}
