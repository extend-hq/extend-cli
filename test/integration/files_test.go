package integration

import (
	"strings"
	"testing"
)

// TestFilesUploadAndDelete exercises the upload + delete lifecycle. Files
// support DELETE on the server, so this test always cleans up after itself
// regardless of test outcome.
func TestFilesUploadAndDelete(t *testing.T) {
	env := requireEnv(t)
	res := runExtend(t, env, "files", "upload", "testdata/sample.txt", "-o", "json")
	res.requireOK(t, "files", "upload")

	var uploaded struct {
		ID     string `json:"id"`
		Object string `json:"object"`
		Name   string `json:"name"`
		Type   string `json:"type"`
	}
	res.decodeJSON(t, &uploaded)

	if !strings.HasPrefix(uploaded.ID, "file_") {
		t.Fatalf("expected uploaded.id with file_ prefix, got %q", uploaded.ID)
	}
	rememberCleanup(t, env, "delete file", "files", "delete", uploaded.ID, "-y")

	if uploaded.Name != "sample.txt" {
		t.Errorf("uploaded.name = %q, want sample.txt", uploaded.Name)
	}
	if uploaded.Type != "TXT" {
		t.Errorf("uploaded.type = %q, want TXT", uploaded.Type)
	}
}

// TestFilesGet verifies that GET /files/:id returns a presigned download URL
// and the file metadata round-trips through our typed File struct via the
// CLI's JSON output.
func TestFilesGet(t *testing.T) {
	env := requireEnv(t)
	uploadRes := runExtend(t, env, "files", "upload", "testdata/sample.txt", "-o", "json")
	uploadRes.requireOK(t, "files", "upload")

	var uploaded struct {
		ID string `json:"id"`
	}
	uploadRes.decodeJSON(t, &uploaded)
	rememberCleanup(t, env, "delete file", "files", "delete", uploaded.ID, "-y")

	getRes := runExtend(t, env, "files", "get", uploaded.ID, "-o", "json")
	getRes.requireOK(t, "files", "get", uploaded.ID)

	var got map[string]any
	getRes.decodeJSON(t, &got)

	if id, _ := got["id"].(string); id != uploaded.ID {
		t.Errorf("get.id = %q, want %q", id, uploaded.ID)
	}
	if presigned, _ := got["presignedUrl"].(string); presigned == "" {
		t.Errorf("expected presignedUrl on full GET; got: %s", getRes.Stdout)
	}
}

// TestFilesGet_RawTextReturnsContents verifies that --raw-text on `files get`
// populates the response's `contents.rawText` field with the parsed text. The
// server caches parsed text after a parse run completes; an unparsed file
// has empty contents, so this test runs a parse first.
//
// Costs credits — gated behind EXTEND_TEST_RUN_OPS=1.
func TestFilesGet_RawTextReturnsContents(t *testing.T) {
	env := requireEnv(t)
	requireRunOps(t)

	upRes := runExtend(t, env, "files", "upload", "testdata/sample.txt", "-o", "json")
	upRes.requireOK(t, "files", "upload")
	var uploaded struct {
		ID string `json:"id"`
	}
	upRes.decodeJSON(t, &uploaded)
	rememberCleanup(t, env, "delete file", "files", "delete", uploaded.ID, "-y")

	// Trigger a parse run so the server caches rawText/markdown for the file.
	parseRes := runExtend(t, env,
		"parse", uploaded.ID,
		"--target", "markdown",
		"--timeout", "2m",
		"-o", "json",
	)
	parseRes.requireOK(t, "parse", uploaded.ID)

	getRes := runExtend(t, env, "files", "get", uploaded.ID, "--raw-text", "-o", "json")
	getRes.requireOK(t, "files", "get", uploaded.ID, "--raw-text")

	var got map[string]any
	getRes.decodeJSON(t, &got)
	contents, ok := got["contents"].(map[string]any)
	if !ok {
		t.Fatalf("contents missing on raw-text GET: %s", getRes.Stdout)
	}
	rawText, _ := contents["rawText"].(string)
	if !strings.Contains(rawText, "INV-12345") {
		t.Errorf("contents.rawText should include sample fixture content; got %q", rawText)
	}
}

// TestFilesList exercises the list endpoint paged. Always safe.
func TestFilesList(t *testing.T) {
	env := requireEnv(t)
	res := runExtend(t, env, "files", "list", "--limit", "5", "-o", "json")
	res.requireOK(t, "files", "list")

	// Files list returns a paginated envelope `{data:[], nextPageToken}`,
	// not a bare array — distinct from extractors/workflows which return
	// the bare data array on `-o json`. Verify shape.
	var page struct {
		Data []map[string]any `json:"data"`
	}
	res.decodeJSON(t, &page)
	for i, f := range page.Data {
		if obj, _ := f["object"].(string); obj != "file" {
			t.Errorf("item %d object = %q, want file", i, obj)
		}
	}
}
