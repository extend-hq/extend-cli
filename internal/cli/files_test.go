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
