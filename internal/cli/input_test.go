package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestUploadOrResolveWith_URLPassword verifies --password flows into the
// FileRef.settings.password field for URL inputs (the only case the server
// honors).
func TestUploadOrResolveWith_URLPassword(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit by uploadOrResolveWith for URL inputs")
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ref, err := uploadOrResolveWith(context.Background(), ta.app, cli,
		"https://example.com/protected.pdf", "secret-passphrase")
	if err != nil {
		t.Fatalf("uploadOrResolveWith: %v", err)
	}
	if ref.URL != "https://example.com/protected.pdf" {
		t.Errorf("URL = %q, want example.com URL", ref.URL)
	}
	if ref.Settings == nil || ref.Settings.Password != "secret-passphrase" {
		t.Errorf("Settings.Password not set: %+v", ref.Settings)
	}
}

// TestUploadOrResolveWith_PasswordRejectedForFileID verifies that --password
// on a file_xxx ID input is a clear CLI error rather than a silent no-op.
// (FileFromIdSchema has no `settings` field on the wire.)
func TestUploadOrResolveWith_PasswordRejectedForFileID(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when CLI validation fails")
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = uploadOrResolveWith(context.Background(), ta.app, cli,
		"file_xK9", "some-password")
	if err == nil || !strings.Contains(err.Error(), "URL inputs") {
		t.Errorf("expected clear --password rejection for file ID, got %v", err)
	}
}

// TestUploadOrResolveWith_PasswordRejectedForLocalUpload verifies that
// --password combined with a local file path errors before the upload
// happens (so we don't waste a round-trip).
func TestUploadOrResolveWith_PasswordRejectedForLocalUpload(t *testing.T) {
	tmp := t.TempDir() + "/local.pdf"
	if err := writeFileForTest(tmp, []byte("%PDF-1.4 fake")); err != nil {
		t.Fatal(err)
	}
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when CLI validation fails")
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = uploadOrResolveWith(context.Background(), ta.app, cli, tmp, "pwd")
	if err == nil || !strings.Contains(err.Error(), "URL inputs") {
		t.Errorf("expected clear --password rejection for local upload, got %v", err)
	}
}

// TestUploadOrResolveWith_NoPasswordPreservesURLRef confirms the no-password
// path still produces a URL FileRef without a Settings object.
func TestUploadOrResolveWith_NoPasswordPreservesURLRef(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit")
	})
	ta := newTestApp(t, srv)
	cli, err := ta.app.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ref, err := uploadOrResolveWith(context.Background(), ta.app, cli,
		"https://example.com/x.pdf", "")
	if err != nil {
		t.Fatalf("uploadOrResolveWith: %v", err)
	}
	if ref.Settings != nil {
		t.Errorf("Settings should be nil when no password set, got %+v", ref.Settings)
	}
}
