package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// stubCmdWithCtx returns a *cobra.Command whose context is set, suitable
// for tests that call into RunE-style functions which expect a Cobra
// command but don't actually need a real command tree. The returned
// command has its name set so renderListForCmd's pagination hint, if
// triggered, renders a recognizable command path.
func stubCmdWithCtx(ctx context.Context, name string) *cobra.Command {
	c := &cobra.Command{Use: name}
	c.SetContext(ctx)
	return c
}

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
	mu       sync.Mutex // guards requests; concurrent handler invocations append
	requests []recordedRequest
	handler  http.HandlerFunc
}

func newFakeServer(t *testing.T, h http.HandlerFunc) *fakeServer {
	t.Helper()
	fs := &fakeServer{t: t, handler: h}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		fs.mu.Lock()
		fs.requests = append(fs.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Header: r.Header.Clone(),
			Body:   body,
		})
		fs.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		fs.handler(w, r)
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeServer) URL() string { return fs.srv.URL }

func (fs *fakeServer) lastRequest() recordedRequest {
	fs.t.Helper()
	fs.mu.Lock()
	defer fs.mu.Unlock()
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

// findCmd builds the full RootDoc tree for the given app, validates it
// (via Build), finds the cobra.Command at the requested path, and detaches
// it from its parent before returning. This is the canonical test entry
// point for tests that exercise a single command:
//
//   - The full RootDoc tree is constructed and validated, so SeeAlso
//     references resolve cross-doc and contract violations anywhere in
//     the tree are caught before any test runs.
//   - The returned command is detached from its parent, so cmd.Execute()
//     runs the leaf in isolation rather than walking up to root and
//     printing root help when called with no positional args. This
//     matches the historical behaviour of the per-command wrapper
//     constructors (newExtractCommand etc.) that this helper replaces.
//
// Path elements are space-free: findCmd(t, app, "extract") for a
// top-level leaf, findCmd(t, app, "extract", "batch") for a subcommand,
// findCmd(t, app, "extractors", "versions", "create") for nested
// resource commands.
func findCmd(t *testing.T, app *App, path ...string) *cobra.Command {
	t.Helper()
	root := RootDoc(app).Build()
	cmd, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("findCmd %v: %v", path, err)
	}
	if parent := cmd.Parent(); parent != nil {
		parent.RemoveCommand(cmd)
	}
	return cmd
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
