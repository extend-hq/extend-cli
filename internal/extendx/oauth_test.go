package extendx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSource is a scripted BearerSource: it serves tokens in order and
// records refresh calls.
type fakeSource struct {
	token        string
	refreshed    string
	refreshCalls int
	refreshErr   error
}

func (f *fakeSource) AccessToken(ctx context.Context) (string, error) {
	return f.token, nil
}

func (f *fakeSource) ForceRefresh(ctx context.Context, rejected string) (string, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return "", f.refreshErr
	}
	return f.refreshed, nil
}

func TestBearerTransportAttachesToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newBearerTransport(nil, &fakeSource{token: "eoat_a"})}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if got != "Bearer eoat_a" {
		t.Errorf("Authorization = %q, want Bearer eoat_a", got)
	}
}

func TestBearerTransportRetriesOnceOn401(t *testing.T) {
	var seen []string
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(seen) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":"UNAUTHORIZED"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `ok`)
	}))
	defer srv.Close()

	src := &fakeSource{token: "eoat_stale", refreshed: "eoat_fresh"}
	client := &http.Client{Transport: newBearerTransport(nil, src)}

	// bytes.Reader bodies populate GetBody, so the retry can replay it.
	resp, err := client.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{"n":1}`)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after retry", resp.StatusCode)
	}
	if src.refreshCalls != 1 {
		t.Errorf("refresh calls = %d, want 1", src.refreshCalls)
	}
	if len(seen) != 2 || seen[0] != "Bearer eoat_stale" || seen[1] != "Bearer eoat_fresh" {
		t.Errorf("auth headers = %v", seen)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || bodies[0] != `{"n":1}` {
		t.Errorf("request body not replayed on retry: %v", bodies)
	}
}

func TestBearerTransportNoRetryOnSecond401(t *testing.T) {
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	src := &fakeSource{token: "eoat_a", refreshed: "eoat_b"}
	client := &http.Client{Transport: newBearerTransport(nil, src)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want the second 401 surfaced", resp.StatusCode)
	}
	if count != 2 {
		t.Errorf("server hits = %d, want exactly 2 (one retry)", count)
	}
	if src.refreshCalls != 1 {
		t.Errorf("refresh calls = %d, want 1", src.refreshCalls)
	}
}

func TestBearerTransportSurfacesRefreshFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	wantErr := errors.New("login expired")
	src := &fakeSource{token: "eoat_a", refreshErr: wantErr}
	client := &http.Client{Transport: newBearerTransport(nil, src)}
	_, err := client.Get(srv.URL)
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrapped refresh failure", err)
	}
}

func TestBearerTransportSkipsRetryForUnreplayableBody(t *testing.T) {
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	src := &fakeSource{token: "eoat_a", refreshed: "eoat_b"}
	client := &http.Client{Transport: newBearerTransport(nil, src)}

	// A raw io.Reader (not bytes/strings.Reader) leaves GetBody nil.
	req, err := http.NewRequest(http.MethodPost, srv.URL, io.NopCloser(strings.NewReader("stream")))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want the 401 to stand", resp.StatusCode)
	}
	if count != 1 {
		t.Errorf("server hits = %d, want 1 (no retry without GetBody)", count)
	}
	if src.refreshCalls != 0 {
		t.Errorf("refresh calls = %d, want 0", src.refreshCalls)
	}
}

func TestNewClientAcceptsOAuthWithoutKey(t *testing.T) {
	cli, err := NewClient(Config{OAuth: &fakeSource{token: "eoat_a"}})
	if err != nil {
		t.Fatalf("NewClient with OAuth source: %v", err)
	}
	if cli == nil {
		t.Fatal("client is nil")
	}
}

func TestNewClientStillRequiresSomeAuth(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("NewClient with neither key nor OAuth should error")
	}
}
