package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
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
		NewClient: func() (*sdkclient.Client, error) {
			return extendx.NewClient(extendx.Config{
				APIKey:  "test-key",
				BaseURL: srv.URL(),
			})
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
	// The Fern-generated SDK validates a discriminator literal
	// (`"object": "extract_run"` and friends) on every typed
	// UnmarshalJSON. Test fixtures historically didn't bother
	// setting it because the hand-rolled client never validated.
	// To keep the tests focused on behavior rather than serialization
	// minutiae, we inject the discriminator on every map at every
	// nesting level when the caller didn't provide one. A test that
	// wants a non-default literal (e.g. explicitly exercising the SDK
	// rejection path) can still pass `"object"` in the map directly.
	autoInjectObject(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// autoInjectObject walks v recursively (objects + arrays) and sets
// "object" on any map[string]any whose id-prefix the inferrer
// recognises. Idempotent — values the caller already set are left
// alone — and safe to call on any JSON-shaped value (it noops on
// primitives).
func autoInjectObject(v any) {
	switch x := v.(type) {
	case map[string]any:
		if _, has := x["object"]; !has {
			if obj := inferObjectFromMap(x); obj != "" {
				x["object"] = obj
			}
		}
		for _, child := range x {
			autoInjectObject(child)
		}
	case []any:
		for _, item := range x {
			autoInjectObject(item)
		}
	case []map[string]any:
		for _, item := range x {
			autoInjectObject(item)
		}
	}
}

// inferObjectFromMap maps a response object back to its SDK
// discriminator literal based on the `id` field's prefix. Returns ""
// when no rule matches; the caller is then responsible for setting
// `object` explicitly (or accepting that the SDK will refuse to
// unmarshal the response).
func inferObjectFromMap(m map[string]any) string {
	id, _ := m["id"].(string)
	if id == "" {
		// Some responses have no id (list envelopes etc.); check
		// for marker fields the caller could plausibly mean.
		if _, ok := m["data"]; ok {
			return "list"
		}
		return ""
	}
	switch {
	case strings.HasPrefix(id, "exr_"):
		return "extract_run"
	case strings.HasPrefix(id, "pr_"):
		return "parse_run"
	case strings.HasPrefix(id, "clr_"):
		return "classify_run"
	case strings.HasPrefix(id, "splr_"):
		return "split_run"
	case strings.HasPrefix(id, "workflow_run_"):
		return "workflow_run"
	case strings.HasPrefix(id, "edr_"):
		return "edit_run"
	case strings.HasPrefix(id, "bpr_"), strings.HasPrefix(id, "bpar_"):
		return "batch_run"
	case strings.HasPrefix(id, "file_"):
		return "file"
	case strings.HasPrefix(id, "ex_"), strings.HasPrefix(id, "ext_"):
		return "extractor"
	case strings.HasPrefix(id, "exv_"):
		return "extractor_version"
	case strings.HasPrefix(id, "cl_"):
		return "classifier"
	case strings.HasPrefix(id, "clv_"):
		return "classifier_version"
	case strings.HasPrefix(id, "spl_"):
		return "splitter"
	case strings.HasPrefix(id, "splv_"):
		return "splitter_version"
	case strings.HasPrefix(id, "workflow_version_"):
		return "workflow_version"
	case strings.HasPrefix(id, "workflow_"):
		return "workflow"
	case strings.HasPrefix(id, "whe_"), strings.HasPrefix(id, "we_"), strings.HasPrefix(id, "wh_"):
		return "webhook_endpoint"
	case strings.HasPrefix(id, "whs_"), strings.HasPrefix(id, "ws_"):
		return "webhook_subscription"
	case strings.HasPrefix(id, "evs_"), strings.HasPrefix(id, "ev_"):
		return "evaluation_set"
	case strings.HasPrefix(id, "esi_"), strings.HasPrefix(id, "evi_"):
		return "evaluation_set_item"
	case strings.HasPrefix(id, "esr_"):
		return "evaluation_set_run"
	}
	return ""
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"code":      code,
		"message":   message,
		"requestId": "apireq_test",
	})
}
