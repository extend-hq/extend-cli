package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeDeviceServer serves the device authorization and token endpoints
// of the RFC 8628 flow. pendingPolls token calls answer
// authorization_pending (after an optional slow_down) before the
// terminal response.
type fakeDeviceServer struct {
	srv          *httptest.Server
	pendingPolls int32
	slowDowns    int32
	// tokenErr, when set, is the terminal token-endpoint error code
	// once the pending polls run out; empty issues tokens.
	tokenErr string
	// polls counts token-endpoint calls.
	polls atomic.Int32
	// lastForm captures the last token request's form values.
	lastGrantType  string
	lastDeviceCode string
	lastResource   string
}

func newFakeDeviceServer(t *testing.T) *fakeDeviceServer {
	t.Helper()
	f := &fakeDeviceServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/device_authorization", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if got := r.PostForm.Get("client_id"); got != "extend-cli" {
			t.Errorf("device authorization client_id = %q", got)
		}
		fmt.Fprint(w, `{
			"device_code": "dev-code-1",
			"user_code": "ABCD-EFGH",
			"verification_uri": "https://id.example/device",
			"verification_uri_complete": "https://id.example/device?user_code=ABCD-EFGH",
			"expires_in": 300,
			"interval": 0
		}`)
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.polls.Add(1)
		f.lastGrantType = r.PostForm.Get("grant_type")
		f.lastDeviceCode = r.PostForm.Get("device_code")
		f.lastResource = r.PostForm.Get("resource")
		if atomic.AddInt32(&f.slowDowns, -1) >= 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"slow_down"}`)
			return
		}
		if atomic.AddInt32(&f.pendingPolls, -1) >= 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"authorization_pending"}`)
			return
		}
		if f.tokenErr != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, f.tokenErr)
			return
		}
		fmt.Fprint(w, `{"access_token":"eoat_dev","refresh_token":"eort_dev","token_type":"Bearer","expires_in":3600}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDeviceServer) client() *Client {
	eps := DefaultEndpoints(f.srv.URL)
	// Not part of the defaults: the device endpoint only ever comes
	// from discovery metadata.
	eps.DeviceAuthorization = f.srv.URL + "/oauth2/device_authorization"
	return &Client{
		HTTPClient: f.srv.Client(),
		Endpoints:  eps,
		ClientID:   "extend-cli",
		Resource:   "https://api.example",
	}
}

// deviceAuth runs DeviceAuthorize and pins the interval to keep the
// poll loop fast in tests (a zero-interval response defaults to the
// RFC's 5s otherwise).
func (f *fakeDeviceServer) deviceAuth(t *testing.T, c *Client) *DeviceAuthorization {
	t.Helper()
	da, err := c.DeviceAuthorize(context.Background())
	if err != nil {
		t.Fatalf("DeviceAuthorize: %v", err)
	}
	da.Interval = 1
	return da
}

func TestDeviceAuthorize(t *testing.T) {
	f := newFakeDeviceServer(t)
	da, err := f.client().DeviceAuthorize(context.Background())
	if err != nil {
		t.Fatalf("DeviceAuthorize: %v", err)
	}
	if da.UserCode != "ABCD-EFGH" || da.DeviceCode != "dev-code-1" {
		t.Errorf("codes = %+v", da)
	}
	if da.VerificationURI != "https://id.example/device" {
		t.Errorf("VerificationURI = %q", da.VerificationURI)
	}
	if !strings.Contains(da.VerificationURIComplete, "user_code=ABCD-EFGH") {
		t.Errorf("VerificationURIComplete = %q", da.VerificationURIComplete)
	}
}

func TestDeviceAuthorizeRequiresEndpoint(t *testing.T) {
	c := &Client{HTTPClient: http.DefaultClient, ClientID: "extend-cli"}
	if _, err := c.DeviceAuthorize(context.Background()); err == nil {
		t.Fatal("DeviceAuthorize without an endpoint must fail")
	}
}

func TestDeviceAuthorizeSanitizesTerminalOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"device_code": "dev-code-1",
			"user_code": "AB\u001b[31mCD",
			"verification_uri": "https://id.example/device\u001b]0;pwned\u0007"
		}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{
		HTTPClient: srv.Client(),
		Endpoints:  Endpoints{DeviceAuthorization: srv.URL + "/device"},
		ClientID:   "extend-cli",
	}
	da, err := c.DeviceAuthorize(context.Background())
	if err != nil {
		t.Fatalf("DeviceAuthorize: %v", err)
	}
	for name, val := range map[string]string{"user_code": da.UserCode, "verification_uri": da.VerificationURI} {
		if strings.ContainsRune(val, '\x1b') || strings.ContainsRune(val, '\a') {
			t.Errorf("%s leaked an escape sequence: %q", name, val)
		}
	}
}

func TestPollDeviceTokenSucceedsAfterPending(t *testing.T) {
	f := newFakeDeviceServer(t)
	f.pendingPolls = 2
	c := f.client()

	tr, err := c.PollDeviceToken(context.Background(), f.deviceAuth(t, c))
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tr.AccessToken != "eoat_dev" || tr.RefreshToken != "eort_dev" {
		t.Errorf("tokens = %+v", tr)
	}
	if got := f.polls.Load(); got != 3 {
		t.Errorf("token polls = %d, want 3 (2 pending + success)", got)
	}
	if f.lastGrantType != DeviceCodeGrant {
		t.Errorf("grant_type = %q", f.lastGrantType)
	}
	if f.lastDeviceCode != "dev-code-1" {
		t.Errorf("device_code = %q", f.lastDeviceCode)
	}
	if f.lastResource != "https://api.example" {
		t.Errorf("resource = %q", f.lastResource)
	}
}

func TestPollDeviceTokenHonorsSlowDown(t *testing.T) {
	f := newFakeDeviceServer(t)
	f.slowDowns = 1
	c := f.client()

	start := time.Now()
	if _, err := c.PollDeviceToken(context.Background(), f.deviceAuth(t, c)); err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	// slow_down adds 5s to the 1s test interval, so the success poll
	// cannot land before ~6s.
	if elapsed := time.Since(start); elapsed < 5*time.Second {
		t.Errorf("second poll after %v, want the slow_down-backed interval (>=6s)", elapsed)
	}
}

func TestPollDeviceTokenTerminalErrors(t *testing.T) {
	for _, code := range []string{"access_denied", "expired_token"} {
		t.Run(code, func(t *testing.T) {
			f := newFakeDeviceServer(t)
			f.tokenErr = code
			c := f.client()

			_, err := c.PollDeviceToken(context.Background(), f.deviceAuth(t, c))
			var te *TokenError
			if !errors.As(err, &te) || te.Code != code {
				t.Fatalf("err = %v, want TokenError %s", err, code)
			}
			if got := f.polls.Load(); got != 1 {
				t.Errorf("token polls = %d, want 1 (terminal errors must stop the loop)", got)
			}
		})
	}
}

func TestPollDeviceTokenStopsWhenCodeExpires(t *testing.T) {
	f := newFakeDeviceServer(t)
	f.pendingPolls = 1000
	c := f.client()

	da := f.deviceAuth(t, c)
	da.ExpiresIn = 1
	_, err := c.PollDeviceToken(context.Background(), da)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded once expires_in elapses", err)
	}
}
