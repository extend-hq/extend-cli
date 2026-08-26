package extendx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	extend "github.com/extend-hq/extend-go-sdk"
	"github.com/extend-hq/extend-go-sdk/client"
)

// FileRef is the CLI-side discriminated union for `file:` inputs.
// Commands convert it into the per-endpoint SDK union type (e.g.
// *extend.ExtractRunsCreateRequestFile) via the BuildFile* helpers in
// this file.
//
// Exactly one of (URL, ID, Text) should be set. Name is honored by URL
// and Text inputs only; ID inputs reuse the original file's name.
// Settings.Password is honored by URL inputs (for password-protected
// PDFs).
type FileRef struct {
	URL      string
	ID       string
	Text     string
	Name     string
	Settings *FileSettings
}

type FileSettings struct {
	Password string
}

// ResolveInput maps a user-supplied input string to a FileRef without
// uploading. The caller is responsible for invoking UploadFile when
// LocalPath is non-empty.
//
// Inputs are recognized in this order:
//
//	"-"                       -> stdin marker (returned as LocalPath="-")
//	file_xxx                  -> FileRef{ID}
//	http://... or https://... -> FileRef{URL}
//	any other string          -> os.Stat probe; error if no file exists
//
// When stat fails, the wrapped error lists the valid input forms so
// the caller can self-diagnose. The base error is wrapped so callers
// using errors.Is(err, fs.ErrNotExist) still work.
func ResolveInput(input string) (ref FileRef, localPath string, err error) {
	switch {
	case input == "-":
		return FileRef{}, "-", nil
	case strings.HasPrefix(input, "file_"):
		return FileRef{ID: input}, "", nil
	case strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://"):
		return FileRef{URL: input}, "", nil
	}
	if _, statErr := os.Stat(input); statErr == nil {
		return FileRef{}, input, nil
	} else {
		return FileRef{}, "", fmt.Errorf(
			"input %q is not a local file, file_id, or URL: %w (valid forms: a local path, a file_xxx ID, or an https:// URL)",
			input, statErr,
		)
	}
}

// UploadOptions carries the optional upload-time knobs Files.Upload
// accepts beyond the file bytes: convert-to-PDF (images/Office/HTML are
// converted server-side before storage) and a password to unlock a
// password-protected PDF on ingest.
type UploadOptions struct {
	ConvertToPdf bool
	Password     string
}

// UploadFile opens path and uploads it via the SDK's Files.Upload
// endpoint. The returned *extend.File has its ID populated; the
// caller can pass that ID through FileRef.ID into a subsequent run
// creation.
func UploadFile(ctx context.Context, c *client.Client, path string) (*extend.File, error) {
	return UploadFileWithOptions(ctx, c, path, UploadOptions{})
}

// UploadFileWithOptions is UploadFile plus the optional upload knobs.
func UploadFileWithOptions(ctx context.Context, c *client.Client, path string, opts UploadOptions) (*extend.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return UploadStreamWithOptions(ctx, c, f, filepath.Base(path), GuessContentType(path), opts)
}

// UploadStream uploads an in-memory or piped reader as a multipart
// upload. The SDK's Files.Upload accepts an io.Reader directly; we
// wrap it with the filename and content-type metadata that the server
// uses for type inference.
//
// We pass UploadOption() so the call uses an untimed *http.Client.
// The CLI's default --http-timeout (60s) is hostile to large multipart
// bodies on slow connections — a legitimate 100MB upload would surface
// as a vague "context deadline exceeded" instead of completing. The
// caller's context remains the only deadline.
func UploadStream(ctx context.Context, c *client.Client, body io.Reader, filename, contentType string) (*extend.File, error) {
	return UploadStreamWithOptions(ctx, c, body, filename, contentType, UploadOptions{})
}

// UploadStreamWithOptions is UploadStream plus the optional upload knobs
// (convert-to-PDF, password). The zero UploadOptions reproduces
// UploadStream exactly, so the common auto-upload path is unaffected.
func UploadStreamWithOptions(ctx context.Context, c *client.Client, body io.Reader, filename, contentType string, opts UploadOptions) (*extend.File, error) {
	req := &extend.FilesUploadRequest{}
	if opts.ConvertToPdf {
		req.ConvertToPdf = extend.Bool(true)
	}
	if opts.Password != "" {
		req.Password = extend.String(opts.Password)
	}
	// Wrap with FileParam when we have metadata to attach; the SDK
	// detects the wrapper and uses the filename + content-type for
	// the multipart Part headers. A bare io.Reader produces a part
	// with no Content-Type, which the server tolerates for the
	// common case (PDF auto-detection) but not for ambiguous types
	// like .txt vs .csv.
	var reader io.Reader = body
	if filename != "" || contentType != "" {
		reader = extend.NewFileParam(body, filename, contentType)
	}
	return c.Files.Upload(ctx, reader, req, UploadOption())
}

// DownloadFile fetches a file's contents by ID. It calls the SDK's
// Files.Retrieve to obtain a presigned URL, then streams that URL's
// body to w. Returns the number of bytes written.
//
// We bypass the SDK for the actual byte transfer because the
// presigned URL points to S3, not the Extend API, and so does not
// require (or expect) the Authorization/X-Extend-Workspace-Id headers
// the SDK attaches to every request.
func DownloadFile(ctx context.Context, c *client.Client, id string, w io.Writer) (int64, error) {
	f, err := c.Files.Retrieve(ctx, id, &extend.FilesRetrieveRequest{})
	if err != nil {
		return 0, err
	}
	if f.PresignedURL == nil || *f.PresignedURL == "" {
		return 0, fmt.Errorf("file %s has no presigned URL (may have expired or be unavailable)", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *f.PresignedURL, nil)
	if err != nil {
		return 0, err
	}
	// Use a bare http.Client here (not the SDK's): we are talking to
	// blob storage, not the Extend API.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("download: storage returned %s", resp.Status)
	}
	return io.Copy(w, resp.Body)
}

// GuessContentType returns a best-effort Content-Type for path based on
// its extension. Returns "" when the extension is unrecognized so the
// upload omits the Content-Type part header.
func GuessContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".tiff", ".tif":
		return "image/tiff"
	case ".heic":
		return "image/heic"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".csv":
		return "text/csv"
	case ".txt", ".md":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	}
	return ""
}

// ErrEmptyFileRef is returned by the BuildFile* helpers when the caller
// hands them a zero FileRef. Surface this with a "no input" message
// rather than a cryptic SDK validation error.
var ErrEmptyFileRef = errors.New("file reference has no URL, ID, or Text")

// fileFromRef produces the three possible SDK building blocks from a
// CLI-side FileRef. Exactly one of the three returned pointers will be
// non-nil; if none qualify the caller receives ErrEmptyFileRef.
func fileFromRef(ref FileRef) (fromURL *extend.FileFromURL, fromID *extend.FileFromID, fromText *extend.FileFromText, err error) {
	switch {
	case ref.URL != "":
		out := &extend.FileFromURL{URL: ref.URL}
		if ref.Name != "" {
			out.Name = extend.String(ref.Name)
		}
		if ref.Settings != nil && ref.Settings.Password != "" {
			out.Settings = &extend.FileFromURLSettings{Password: extend.String(ref.Settings.Password)}
		}
		return out, nil, nil, nil
	case ref.ID != "":
		return nil, &extend.FileFromID{ID: ref.ID}, nil, nil
	case ref.Text != "":
		out := &extend.FileFromText{Text: ref.Text}
		if ref.Name != "" {
			out.Name = extend.String(ref.Name)
		}
		return nil, nil, out, nil
	}
	return nil, nil, nil, ErrEmptyFileRef
}

// BuildExtractFile converts a FileRef into the SDK's per-endpoint file
// union for /extract_runs.
func BuildExtractFile(ref FileRef) (*extend.ExtractRunsCreateRequestFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.ExtractRunsCreateRequestFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildParseFile converts a FileRef into the SDK's per-endpoint file
// union for /parse_runs. Parse runs accept URL and ID inputs only
// (the server's schema does not include text inputs); a Text-shaped
// FileRef returns an error.
func BuildParseFile(ref FileRef) (*extend.ParseRunsCreateRequestFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	if fromText != nil {
		return nil, errors.New("parse runs only accept URL or file-ID inputs")
	}
	return &extend.ParseRunsCreateRequestFile{
		FileFromURL: fromURL,
		FileFromID:  fromID,
	}, nil
}

// BuildClassifyFile converts a FileRef into the SDK's per-endpoint
// file union for /classify_runs.
func BuildClassifyFile(ref FileRef) (*extend.ClassifyRunsCreateRequestFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.ClassifyRunsCreateRequestFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildSplitFile converts a FileRef into the SDK's per-endpoint file
// union for /split_runs. The split endpoint accepts URL and ID inputs
// only; passing Text returns an error.
func BuildSplitFile(ref FileRef) (*extend.SplitRunsCreateRequestFile, error) {
	fromURL, fromID, _, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	if fromURL == nil && fromID == nil {
		return nil, errors.New("split runs only accept URL or file-ID inputs")
	}
	return &extend.SplitRunsCreateRequestFile{
		FileFromURL: fromURL,
		FileFromID:  fromID,
	}, nil
}

// BuildEditFile converts a FileRef into the SDK's per-endpoint file
// union for /edit_runs. The edit endpoint accepts URL and ID inputs
// only.
func BuildEditFile(ref FileRef) (*extend.EditRunsCreateRequestFile, error) {
	fromURL, fromID, _, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	if fromURL == nil && fromID == nil {
		return nil, errors.New("edit runs only accept URL or file-ID inputs")
	}
	return &extend.EditRunsCreateRequestFile{
		FileFromURL: fromURL,
		FileFromID:  fromID,
	}, nil
}

// BuildFormDetectionFile converts a FileRef into the SDK's per-endpoint
// file union for /form_detection_runs (URL and ID inputs only).
func BuildFormDetectionFile(ref FileRef) (*extend.FormDetectionRunsCreateRequestFile, error) {
	fromURL, fromID, _, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	if fromURL == nil && fromID == nil {
		return nil, errors.New("form detection runs only accept URL or file-ID inputs")
	}
	return &extend.FormDetectionRunsCreateRequestFile{
		FileFromURL: fromURL,
		FileFromID:  fromID,
	}, nil
}

// BuildWorkflowFile converts a FileRef into the SDK's per-endpoint
// file union for /workflow_runs.
func BuildWorkflowFile(ref FileRef) (*extend.WorkflowRunsCreateRequestFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.WorkflowRunsCreateRequestFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildExtractBatchFile converts a FileRef into the SDK's per-item
// file union for /extract_runs/batch.
func BuildExtractBatchFile(ref FileRef) (*extend.ExtractRunsCreateBatchRequestInputsItemFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.ExtractRunsCreateBatchRequestInputsItemFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildClassifyBatchFile converts a FileRef into the SDK's per-item
// file union for /classify_runs/batch.
func BuildClassifyBatchFile(ref FileRef) (*extend.ClassifyRunsCreateBatchRequestInputsItemFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.ClassifyRunsCreateBatchRequestInputsItemFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildSplitBatchFile converts a FileRef into the SDK's per-item file
// union for /split_runs/batch. Split inputs accept URL or ID only.
func BuildSplitBatchFile(ref FileRef) (*extend.SplitRunsCreateBatchRequestInputsItemFile, error) {
	fromURL, fromID, _, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	if fromURL == nil && fromID == nil {
		return nil, errors.New("split runs only accept URL or file-ID inputs")
	}
	return &extend.SplitRunsCreateBatchRequestInputsItemFile{
		FileFromURL: fromURL,
		FileFromID:  fromID,
	}, nil
}

// BuildParseBatchFile converts a FileRef into the SDK's per-item file
// union for /parse_runs/batch. Unlike the non-batch /parse_runs
// endpoint, the batch input schema accepts text inputs too.
func BuildParseBatchFile(ref FileRef) (*extend.ParseRunsCreateBatchRequestInputsItemFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.ParseRunsCreateBatchRequestInputsItemFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}

// BuildWorkflowBatchFile converts a FileRef into the SDK's per-item
// file union for /workflow_runs/batch.
func BuildWorkflowBatchFile(ref FileRef) (*extend.WorkflowRunsCreateBatchRequestInputsItemFile, error) {
	fromURL, fromID, fromText, err := fileFromRef(ref)
	if err != nil {
		return nil, err
	}
	return &extend.WorkflowRunsCreateBatchRequestInputsItemFile{
		FileFromURL:  fromURL,
		FileFromID:   fromID,
		FileFromText: fromText,
	}, nil
}
