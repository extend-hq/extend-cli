package extendx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInput_Stdin(t *testing.T) {
	ref, local, err := ResolveInput("-")
	if err != nil {
		t.Fatalf("ResolveInput(\"-\") returned %v", err)
	}
	if local != "-" {
		t.Errorf("ResolveInput(\"-\") localPath = %q; want %q", local, "-")
	}
	if (ref != FileRef{}) {
		t.Errorf("ResolveInput(\"-\") ref = %+v; want zero", ref)
	}
}

func TestResolveInput_FileID(t *testing.T) {
	ref, local, err := ResolveInput("file_abc123")
	if err != nil {
		t.Fatalf("ResolveInput(file_id) returned %v", err)
	}
	if local != "" {
		t.Errorf("file_id should not populate localPath, got %q", local)
	}
	if ref.ID != "file_abc123" {
		t.Errorf("ref.ID = %q; want file_abc123", ref.ID)
	}
	if ref.URL != "" || ref.Text != "" {
		t.Errorf("file_id should populate ONLY ID, got %+v", ref)
	}
}

func TestResolveInput_URL(t *testing.T) {
	for _, u := range []string{
		"https://example.com/doc.pdf",
		"http://example.com/doc.pdf",
	} {
		ref, local, err := ResolveInput(u)
		if err != nil {
			t.Fatalf("ResolveInput(%q) returned %v", u, err)
		}
		if local != "" {
			t.Errorf("URL input should not populate localPath, got %q", local)
		}
		if ref.URL != u {
			t.Errorf("ref.URL = %q; want %q", ref.URL, u)
		}
	}
}

func TestResolveInput_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref, local, err := ResolveInput(path)
	if err != nil {
		t.Fatalf("ResolveInput(real file) returned %v", err)
	}
	if local != path {
		t.Errorf("localPath = %q; want %q", local, path)
	}
	if (ref != FileRef{}) {
		t.Errorf("local file ref should be zero-valued, got %+v", ref)
	}
}

func TestResolveInput_NotFound(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.pdf")

	_, _, err := ResolveInput(missing)
	if err == nil {
		t.Fatal("ResolveInput(missing) returned nil error")
	}
	// errors.Is must surface fs.ErrNotExist so callers can detect
	// "no such file" without parsing the message.
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ResolveInput(missing) err = %v; want errors.Is(fs.ErrNotExist)", err)
	}
}

func TestFileFromRef_Empty(t *testing.T) {
	_, _, _, err := fileFromRef(FileRef{})
	if !errors.Is(err, ErrEmptyFileRef) {
		t.Errorf("fileFromRef(empty) err = %v; want ErrEmptyFileRef", err)
	}
}

func TestFileFromRef_URL(t *testing.T) {
	ref := FileRef{
		URL:      "https://example.com/x.pdf",
		Name:     "x.pdf",
		Settings: &FileSettings{Password: "hunter2"},
	}
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	if fromURL == nil {
		t.Fatal("fileFromRef(url) returned nil fromURL")
	}
	if fromID != nil || fromText != nil {
		t.Errorf("expected only fromURL, got id=%v text=%v", fromID, fromText)
	}
	if fromURL.URL != ref.URL {
		t.Errorf("URL = %q; want %q", fromURL.URL, ref.URL)
	}
	if fromURL.Name == nil || *fromURL.Name != "x.pdf" {
		t.Errorf("Name = %v; want pointer to x.pdf", fromURL.Name)
	}
	if fromURL.Settings == nil || fromURL.Settings.Password == nil || *fromURL.Settings.Password != "hunter2" {
		t.Errorf("Settings.Password = %v; want pointer to hunter2", fromURL.Settings)
	}
}

func TestFileFromRef_ID(t *testing.T) {
	fromURL, fromID, fromText, err := fileFromRef(FileRef{ID: "file_abc"})
	if err != nil {
		t.Fatal(err)
	}
	if fromID == nil || fromID.ID != "file_abc" {
		t.Errorf("fromID = %+v; want {ID:file_abc}", fromID)
	}
	if fromURL != nil || fromText != nil {
		t.Errorf("expected only fromID, got url=%v text=%v", fromURL, fromText)
	}
}

func TestFileFromRef_Text(t *testing.T) {
	fromURL, fromID, fromText, err := fileFromRef(FileRef{
		Text: "hello world",
		Name: "greeting.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if fromText == nil {
		t.Fatal("fromText is nil")
	}
	if fromText.Text != "hello world" {
		t.Errorf("Text = %q; want hello world", fromText.Text)
	}
	if fromText.Name == nil || *fromText.Name != "greeting.txt" {
		t.Errorf("Name = %v; want pointer to greeting.txt", fromText.Name)
	}
	if fromURL != nil || fromID != nil {
		t.Errorf("expected only fromText, got url=%v id=%v", fromURL, fromID)
	}
}

func TestFileFromRef_Precedence(t *testing.T) {
	// When multiple fields are set the priority is URL > ID > Text.
	// This lock prevents accidental reordering.
	ref := FileRef{
		URL:  "https://x",
		ID:   "file_x",
		Text: "x",
	}
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	if fromURL == nil || fromID != nil || fromText != nil {
		t.Errorf("URL precedence violated: url=%v id=%v text=%v", fromURL, fromID, fromText)
	}
}

func TestBuildExtractFile(t *testing.T) {
	// Round-trip URL ref through the builder; assert the resulting
	// SDK union carries the URL variant.
	out, err := BuildExtractFile(FileRef{URL: "https://x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.FileFromURL == nil || out.FileFromURL.URL != "https://x" {
		t.Errorf("BuildExtractFile(URL) = %+v; want FileFromURL populated", out)
	}
	if out.FileFromID != nil || out.FileFromText != nil {
		t.Errorf("BuildExtractFile(URL) populated other variants: %+v", out)
	}
}

func TestBuildExtractFile_Text(t *testing.T) {
	// Extract DOES accept text inputs.
	out, err := BuildExtractFile(FileRef{Text: "raw"})
	if err != nil {
		t.Fatal(err)
	}
	if out.FileFromText == nil || out.FileFromText.Text != "raw" {
		t.Errorf("BuildExtractFile(text) = %+v; want FileFromText populated", out)
	}
}

func TestBuildExtractFile_Empty(t *testing.T) {
	_, err := BuildExtractFile(FileRef{})
	if !errors.Is(err, ErrEmptyFileRef) {
		t.Errorf("BuildExtractFile(empty) err = %v; want ErrEmptyFileRef", err)
	}
}

func TestBuildParseFile_RejectsText(t *testing.T) {
	// Parse runs do not accept text inputs. The builder must reject
	// a Text-shaped ref with a useful message.
	_, err := BuildParseFile(FileRef{Text: "raw"})
	if err == nil {
		t.Fatal("BuildParseFile(text) = nil; want non-nil")
	}
}

func TestBuildParseFile_AcceptsURL(t *testing.T) {
	out, err := BuildParseFile(FileRef{URL: "https://x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.FileFromURL == nil || out.FileFromID != nil {
		t.Errorf("BuildParseFile(URL) = %+v; want FileFromURL only", out)
	}
}

func TestBuildSplitFile_RejectsText(t *testing.T) {
	_, err := BuildSplitFile(FileRef{Text: "raw"})
	if err == nil {
		t.Error("BuildSplitFile(text) = nil; want non-nil")
	}
}

func TestBuildEditFile_RejectsText(t *testing.T) {
	_, err := BuildEditFile(FileRef{Text: "raw"})
	if err == nil {
		t.Error("BuildEditFile(text) = nil; want non-nil")
	}
}

func TestBuildEditSchemaFile_RejectsText(t *testing.T) {
	_, err := BuildEditSchemaFile(FileRef{Text: "raw"})
	if err == nil {
		t.Error("BuildEditSchemaFile(text) = nil; want non-nil")
	}
}

func TestBuildFormDetectionFile_RejectsText(t *testing.T) {
	_, err := BuildFormDetectionFile(FileRef{Text: "raw"})
	if err == nil {
		t.Error("BuildFormDetectionFile(text) = nil; want non-nil")
	}
}

func TestBuildWorkflowFile_AcceptsAllInputs(t *testing.T) {
	// Workflows pass the file through to the underlying step;
	// builder accepts URL, ID, and Text.
	for _, ref := range []FileRef{
		{URL: "https://x"},
		{ID: "file_x"},
		{Text: "raw"},
	} {
		_, err := BuildWorkflowFile(ref)
		if err != nil {
			t.Errorf("BuildWorkflowFile(%+v) = %v; want nil", ref, err)
		}
	}
}
