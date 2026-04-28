package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/iostreams"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

type fakeServer struct {
	t        *testing.T
	srv      *httptest.Server
	requests []recordedRequest
	handler  http.HandlerFunc
}

func newFakeServer(t *testing.T, h http.HandlerFunc) *fakeServer {
	t.Helper()
	fs := &fakeServer{t: t, handler: h}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		fs.requests = append(fs.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Header: r.Header.Clone(),
			Body:   body,
		})
		r.Body = io.NopCloser(bytes.NewReader(body))
		fs.handler(w, r)
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeServer) URL() string { return fs.srv.URL }

func (fs *fakeServer) lastRequest() recordedRequest {
	fs.t.Helper()
	if len(fs.requests) == 0 {
		fs.t.Fatal("no requests recorded")
	}
	return fs.requests[len(fs.requests)-1]
}

type testApp struct {
	app    *App
	out    *bytes.Buffer
	errOut *bytes.Buffer
	in     *bytes.Buffer
}

func newTestApp(t *testing.T, srv *fakeServer) *testApp {
	t.Helper()
	ios, in, out, errOut := iostreams.Test()
	ios.SetColorEnabled(false)
	app := &App{
		IO: ios,
		NewClient: func() (*client.Client, error) {
			c := client.New("test-key")
			c.BaseURL = srv.URL()
			return c, nil
		},
	}
	return &testApp{app: app, out: out, errOut: errOut, in: in}
}

func newTTYStreams(t *testing.T) (*iostreams.IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ios, in, out, errOut := iostreams.Test()
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	return ios, in, out, errOut
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"code":      code,
		"message":   message,
		"requestId": "apireq_test",
	})
}
