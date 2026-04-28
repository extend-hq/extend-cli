package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockStorage(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFilesDelete_RejectsWithoutConfirmInNonTTY(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called without confirmation")
	})
	ta := newTestApp(t, srv)
	err := runFilesDelete(context.Background(), ta.app, "file_abc", false)
	if err == nil || !strings.Contains(err.Error(), "refusing to delete") {
		t.Errorf("expected refusal, got %v", err)
	}
}

func TestFilesDelete_WithYesFlag(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/files/file_abc" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(204)
	})
	ta := newTestApp(t, srv)
	if err := runFilesDelete(context.Background(), ta.app, "file_abc", true); err != nil {
		t.Fatalf("runFilesDelete: %v", err)
	}
	if !strings.Contains(ta.out.String(), "Deleted") {
		t.Errorf("expected 'Deleted' in stdout, got: %s", ta.out.String())
	}
}

func TestFilesDownload_StreamingToFile(t *testing.T) {
	tmp := t.TempDir()
	storage := mockStorage(t, "the bytes of the file")

	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/file_abc" {
			t.Fatalf("unexpected GET %s", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"id":           "file_abc",
			"name":         "doc.txt",
			"presignedUrl": storage.URL,
		})
	})
	ta := newTestApp(t, srv)
	out := filepath.Join(tmp, "downloaded.txt")
	if err := runFilesDownload(context.Background(), ta.app, "file_abc", out); err != nil {
		t.Fatalf("runFilesDownload: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "the bytes of the file" {
		t.Errorf("downloaded contents = %q", string(got))
	}
}

func TestFilesDownload_ToStdout(t *testing.T) {
	storage := mockStorage(t, "abc")
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"id":           "file_abc",
			"name":         "x",
			"presignedUrl": storage.URL,
		})
	})
	ta := newTestApp(t, srv)
	if err := runFilesDownload(context.Background(), ta.app, "file_abc", "-"); err != nil {
		t.Fatalf("runFilesDownload: %v", err)
	}
	if got := ta.out.String(); got != "abc" {
		t.Errorf("stdout = %q, want abc", got)
	}
}

// TestFilesList_TableFormatRendersTable is a regression test for the bug
// where `extend files list -o table` returned an error from output.Render
// instead of producing a table. Before the fix the code path would
// short-circuit into renderWithDefault → output.Render(FormatTable) which
// errors with "table format requires RenderTable, not Render".
func TestFilesList_TableFormatRendersTable(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"data": []map[string]any{
				{"id": "file_a", "name": "doc.pdf", "type": "PDF", "createdAt": "2025-01-01T00:00:00Z"},
				{"id": "file_b", "name": "img.png", "type": "PNG", "createdAt": "2025-01-02T00:00:00Z"},
			},
		})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "table"
	if err := runFilesList(context.Background(), ta.app, "", 20, false, "desc"); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := ta.out.String()
	for _, want := range []string{"ID", "NAME", "TYPE", "CREATED", "file_a", "file_b", "doc.pdf", "img.png"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

// TestFilesList_MarkdownFormatRendersTable is a regression test for the bug
// where `extend files list -o markdown` returned "unsupported format"
// because the renderWithDefault path delegated to output.Render which only
// knows json/yaml/raw/id.
func TestFilesList_MarkdownFormatRendersTable(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"data": []map[string]any{
				{"id": "file_a", "name": "doc.pdf", "type": "PDF", "createdAt": "2025-01-01T00:00:00Z"},
			},
		})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "markdown"
	if err := runFilesList(context.Background(), ta.app, "", 20, false, "desc"); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := ta.out.String()
	for _, want := range []string{"| ID | NAME | TYPE | CREATED |", "| --- | --- | --- | --- |", "| file_a |"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q:\n%s", want, out)
		}
	}
}

// TestFilesList_IDFormat is a regression test for the bug where
// `extend files list -o id` errored with
// "--output id requires payload with an 'id' field; got map[string]interface {}".
// The bug was that renderList delegated -o id to output.Render which then
// tried to extract an 'id' from the page envelope ({data:[],nextPageToken:""})
// rather than emitting one ID per item.
func TestFilesList_IDFormat(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"data": []map[string]any{
				{"id": "file_a", "name": "doc.pdf", "type": "PDF", "createdAt": "2025-01-01T00:00:00Z"},
				{"id": "file_b", "name": "img.png", "type": "PNG", "createdAt": "2025-01-02T00:00:00Z"},
			},
		})
	})
	ta := newTestApp(t, srv)
	ta.app.Format = "id"
	if err := runFilesList(context.Background(), ta.app, "", 20, false, "desc"); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := ta.out.String()
	want := "file_a\nfile_b\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
