package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// packageServer accepts uploads, naming each file after the uploaded
// file name so tests can check order despite parallel uploads, and
// answers run creation with a run over three files.
func packageServer(t *testing.T, createPath string, run map[string]any) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse upload: %v", err)
			}
			var name string
			for _, fh := range r.MultipartForm.File {
				name = fh[0].Filename
			}
			writeJSON(w, 200, map[string]any{"object": "file", "id": "file_" + strings.TrimSuffix(name, ".pdf"), "name": name, "type": "PDF"})
		case r.Method == http.MethodPost && r.URL.Path == createPath:
			writeJSON(w, 200, run)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func createBody(t *testing.T, srv *fakeServer, path string) map[string]any {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, r := range srv.requests {
		if r.Method == http.MethodPost && r.Path == path {
			var body map[string]any
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			return body
		}
	}
	t.Fatalf("no POST %s was made", path)
	return nil
}

func writePDFs(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
		if err := os.WriteFile(paths[i], []byte("%PDF-1.7"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

var threeFiles = []map[string]any{
	{"id": "file_msa", "name": "msa.pdf"},
	{"id": "file_amend1", "name": "amend1.pdf"},
	{"id": "file_c", "name": "c.pdf"},
}

func TestExtract_PackageSendsEveryInputInOrder(t *testing.T) {
	srv := packageServer(t, "/extract_runs", map[string]any{"id": "exr_p", "status": "PROCESSING", "files": threeFiles})
	ta := newTestApp(t, srv)
	paths := writePDFs(t, "msa.pdf", "amend1.pdf")
	cmd := findCmd(t, ta.app, "extract")
	cmd.SetArgs([]string{paths[0], paths[1], "file_c", "https://example.com/d.pdf", "--package", "--using", "ex_1", "--wait=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("extract --package: %v", err)
	}
	body := createBody(t, srv, "/extract_runs")
	if _, ok := body["file"]; ok {
		t.Errorf("body has file alongside package; the API rejects both: %v", body)
	}
	want := []any{
		map[string]any{"id": "file_msa"},
		map[string]any{"id": "file_amend1"},
		map[string]any{"id": "file_c"},
		map[string]any{"url": "https://example.com/d.pdf"},
	}
	pkg, _ := body["package"].(map[string]any)
	if got := pkg["files"]; !reflect.DeepEqual(got, want) {
		t.Errorf("package.files = %v, want %v", got, want)
	}
	if !strings.Contains(ta.out.String(), "file_amend1") {
		t.Errorf("stdout = %q, want the run's files", ta.out.String())
	}
}

func TestWorkflowsRun_PackageFromFilesFromShowsFileCount(t *testing.T) {
	srv := packageServer(t, "/workflow_runs", map[string]any{"id": "workflow_run_p", "status": "PROCESSING", "files": threeFiles})
	ta := newTestApp(t, srv)
	list := filepath.Join(t.TempDir(), "inputs.txt")
	if err := os.WriteFile(list, []byte("file_msa\n\nhttps://example.com/amend1.pdf\nfile_c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(t, ta.app, "workflows", "run")
	cmd.SetArgs([]string{"--package", "--files-from", list, "--using", "workflow_1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("workflows run --package: %v", err)
	}
	body := createBody(t, srv, "/workflow_runs")
	want := []any{
		map[string]any{"id": "file_msa"},
		map[string]any{"url": "https://example.com/amend1.pdf"},
		map[string]any{"id": "file_c"},
	}
	pkg, _ := body["package"].(map[string]any)
	if got := pkg["files"]; !reflect.DeepEqual(got, want) {
		t.Errorf("package.files = %v, want %v", got, want)
	}
	if _, ok := body["file"]; ok {
		t.Errorf("body has file alongside package: %v", body)
	}
	if !strings.Contains(ta.out.String(), "workflow_run_p (PROCESSING, 3 files, 0 steps)") {
		t.Errorf("stdout = %q, want the file count in the headline", ta.out.String())
	}
}

func TestPackage_ArgumentRules(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called, got %s %s", r.Method, r.URL.Path)
	})
	fiftyOne := make([]string, 51)
	for i := range fiftyOne {
		fiftyOne[i] = fmt.Sprintf("file_%d", i)
	}
	cases := []struct {
		path []string
		args []string
		want string
	}{
		{[]string{"extract"}, []string{"file_a", "file_b", "--using", "ex_1"},
			"extract takes one input, got 2: pass --package to process them together in one run, or use 'extend extract batch'"},
		{[]string{"extract"}, []string{"--files-from", "list.txt", "--using", "ex_1"},
			"--files-from needs --package; to run each listed file on its own, use 'extend extract batch --files-from'"},
		{[]string{"extract"}, []string{"file_a", "--package", "--using", "ex_1"},
			"--package needs at least 2 inputs"},
		{[]string{"extract"}, append(fiftyOne, "--package", "--using", "ex_1"),
			"--package takes at most 50 inputs, got 51"},
		{[]string{"extract"}, []string{"file_a", "file_b", "file_a", "--package", "--using", "ex_1"},
			"file_a is listed twice"},
		{[]string{"extract"}, []string{"--package", "--text", "hi", "--using", "ex_1"},
			"--text can't be used with --package"},
		{[]string{"workflows", "run"}, []string{"file_a", "file_b", "--using", "workflow_1"},
			"workflows run takes one input, got 2: pass --package to process them together in one run, or use 'extend workflows run batch'"},
		{[]string{"workflows", "run"}, []string{"file_a", "file_b", "--package", "--using", "workflow_1", "--outputs", "[]"},
			"--outputs can't be used with --package"},
	}
	for _, tc := range cases {
		ta := newTestApp(t, srv)
		cmd := findCmd(t, ta.app, tc.path...)
		cmd.SetArgs(tc.args)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("extend %s %v: err = %v, want %q", strings.Join(tc.path, " "), tc.args, err, tc.want)
		}
	}
}
