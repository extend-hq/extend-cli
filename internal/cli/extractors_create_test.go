package cli

import (
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

var generatedExtractor = map[string]any{"object": "extractor", "id": "ex_gen", "name": "Invoices"}

func TestExtractorsCreate_GeneratesTheSchemaFromSamples(t *testing.T) {
	srv := packageServer(t, "/extractors", generatedExtractor)
	ta := newTestApp(t, srv)
	paths := writePDFs(t, "inv1.pdf")
	cmd := findCmd(t, ta.app, "extractors", "create")
	cmd.SetArgs([]string{"--name", "Invoices", "--sample", paths[0], "--sample", "file_b",
		"--sample", "https://example.com/c.pdf", "--instructions", "Vendor invoices"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("extractors create --sample: %v", err)
	}
	body := createBody(t, srv, "/extractors")
	want := map[string]any{
		"name": "Invoices",
		"generate": map[string]any{
			"files": []any{
				map[string]any{"id": "file_inv1"},
				map[string]any{"id": "file_b"},
				map[string]any{"url": "https://example.com/c.pdf"},
			},
			"instructions": "Vendor invoices",
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
	if !strings.Contains(ta.errOut.String(), "Generating a schema from 3 samples") {
		t.Errorf("stderr = %q, want the generation notice", ta.errOut.String())
	}
	if !strings.Contains(ta.out.String(), "ex_gen") {
		t.Errorf("stdout = %q, want the new extractor", ta.out.String())
	}
}

// Generation takes minutes and is billed, so it must outlast the
// per-request HTTP timeout and must not be retried.
func TestExtractorsCreate_GenerationIgnoresTheHTTPTimeoutAndIsNotRetried(t *testing.T) {
	var creates atomic.Int32
	var fail atomic.Bool
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		time.Sleep(300 * time.Millisecond)
		if fail.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "UNAVAILABLE", "message": "try later"})
			return
		}
		writeJSON(w, 200, generatedExtractor)
	})
	run := func(args ...string) error {
		ta := newTestApp(t, srv)
		ta.app.NewClient = func() (*sdkclient.Client, error) {
			return extendx.NewClient(extendx.Config{APIKey: "test-key", BaseURL: srv.URL(), HTTPTimeout: 100 * time.Millisecond})
		}
		cmd := findCmd(t, ta.app, "extractors", "create")
		cmd.SetArgs(args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return cmd.Execute()
	}

	if err := run("--name", "Invoices", "--from-file", `{"cloneExtractorId":"ex_tmpl"}`); err == nil {
		t.Fatal("a plain create outlived a 100ms --http-timeout; the slow server isn't slow enough to test this")
	}
	if err := run("--name", "Invoices", "--sample", "file_a"); err != nil {
		t.Fatalf("generation failed on a 100ms --http-timeout: %v", err)
	}

	fail.Store(true)
	creates.Store(0)
	if err := run("--name", "Invoices", "--sample", "file_a"); err == nil {
		t.Fatal("generation succeeded against a failing server")
	}
	if n := creates.Load(); n != 1 {
		t.Errorf("made %d generation requests, want 1 (no retries)", n)
	}
}

func TestExtractorsCreate_TimeoutStopsGeneration(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, 200, generatedExtractor)
	})
	ta := newTestApp(t, srv)
	cmd := findCmd(t, ta.app, "extractors", "create")
	cmd.SetArgs([]string{"--name", "Invoices", "--sample", "file_a", "--timeout", "50ms"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "schema generation didn't finish within 50ms") {
		t.Fatalf("err = %v, want the generation timeout", err)
	}
}

func TestExtractorsCreate_SampleRules(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	six := []string{"--name", "Invoices"}
	for _, id := range []string{"file_1", "file_2", "file_3", "file_4", "file_5", "file_6"} {
		six = append(six, "--sample", id)
	}
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--sample", "file_a"}, "--name is required"},
		{six, "schema generation takes 1 to 5 samples, got 6"},
		{[]string{"--name", "Invoices", "--instructions", "Vendor invoices"}, "--instructions needs --sample"},
		{[]string{"--name", "Invoices", "--sample", "file_a", "--from-file", `{"config":{"schema":{"type":"object"}}}`},
			"can't be combined with a config or cloneExtractorId"},
		{[]string{"--name", "Invoices", "--sample", "file_a", "--from-file", `{"generate":{"files":[{"id":"file_b"}]}}`},
			"with --sample or as generate.files in --from-file, not both"},
		{[]string{"--name", "Invoices", "--sample", "file_a", "--instructions", strings.Repeat("a", 2501)},
			"the instructions are 2501 characters; the limit is 2500"},
		{[]string{"invoice.pdf", "--name", "Invoices"},
			"takes no arguments; to generate the schema from invoice.pdf, pass --sample invoice.pdf"},
	}
	for _, tc := range cases {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, "extractors", "create")
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend extractors create %v: err = %v, want %q", tc.args, err, tc.want)
		}
	}
}
