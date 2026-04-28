package client

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// File covers both PublicFileSummary (returned in run responses, list pages)
// and PublicFile (returned by GET /files/:id with rawText/markdown/html
// query parameters). The Summary form lacks `presignedUrl` and `contents`;
// the full GET form populates them. We use a single struct because the
// fields are otherwise identical and Summary missing keys decode to zero.
type File struct {
	ID           string        `json:"id"`
	Object       string        `json:"object"`
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	ParentFileID string        `json:"parentFileId,omitempty"`
	PresignedURL string        `json:"presignedUrl,omitempty"`
	Contents     *FileContents `json:"contents,omitempty"`
	Metadata     FileMeta      `json:"metadata,omitempty"`
	CreatedAt    string        `json:"createdAt,omitempty"`
	UpdatedAt    string        `json:"updatedAt,omitempty"`
}

// FileMeta carries server-provided file metadata. ParentSplit is populated
// only on files produced by a splitter (each split section becomes a child
// file with a back-reference here).
type FileMeta struct {
	PageCount   int              `json:"pageCount,omitempty"`
	ParentSplit *FileParentSplit `json:"parentSplit,omitempty"`
}

type FileParentSplit struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
	StartPage  int    `json:"startPage"`
	EndPage    int    `json:"endPage"`
}

// FileContents holds the structured per-page/section content the API returns
// when GET /files/:id is called with `?rawText=true`, `?markdown=true`, or
// `?html=true` query parameters. All fields are optional and populated only
// for the formats the caller explicitly requested. Sheets is only present
// for Excel inputs.
type FileContents struct {
	RawText  string                `json:"rawText,omitempty"`
	Markdown string                `json:"markdown,omitempty"`
	Pages    []FileContentsPage    `json:"pages,omitempty"`
	Sections []FileContentsSection `json:"sections,omitempty"`
	Sheets   []FileContentsSheet   `json:"sheets,omitempty"`
}

type FileContentsPage struct {
	PageNumber int     `json:"pageNumber"`
	PageHeight float64 `json:"pageHeight,omitempty"`
	PageWidth  float64 `json:"pageWidth,omitempty"`
	RawText    string  `json:"rawText,omitempty"`
	Markdown   string  `json:"markdown,omitempty"`
	HTML       string  `json:"html,omitempty"`
}

type FileContentsSection struct {
	StartPageNumber int    `json:"startPageNumber"`
	EndPageNumber   int    `json:"endPageNumber"`
	Markdown        string `json:"markdown"`
}

type FileContentsSheet struct {
	SheetName string `json:"sheetName"`
	RawText   string `json:"rawText,omitempty"`
}

// FileRef is the request-side discriminated union for `file:` inputs across
// every run-creation endpoint. Exactly one of (URL, ID, Text, Base64) should
// be set; passing more than one yields server-side validation errors. Name
// is honored by URL/Text/Base64 inputs only; ID inputs reuse the original
// file's name. Settings.Password is honored by URL inputs for password-
// protected PDFs.
type FileRef struct {
	URL      string        `json:"url,omitempty"`
	ID       string        `json:"id,omitempty"`
	Text     string        `json:"text,omitempty"`
	Base64   string        `json:"base64,omitempty"`
	Name     string        `json:"name,omitempty"`
	Settings *FileSettings `json:"settings,omitempty"`
}

type FileSettings struct {
	Password string `json:"password,omitempty"`
}

func (c *Client) UploadFile(ctx context.Context, path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return c.UploadStream(ctx, f, filepath.Base(path), guessContentType(path))
}

func (c *Client) UploadStream(ctx context.Context, body io.Reader, filename, contentType string) (*File, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	errCh := make(chan error, 1)

	go func() {
		defer pw.Close()
		defer mw.Close()

		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		part, err := mw.CreatePart(header)
		if err != nil {
			errCh <- err
			return
		}
		if _, err := io.Copy(part, body); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	resp, err := c.do(ctx, http.MethodPost, "/files/upload", pr, mw.FormDataContentType())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if perr := <-errCh; perr != nil {
		return nil, fmt.Errorf("upload stream: %w", perr)
	}

	var uploaded File
	if err := jsonDecode(resp.Body, &uploaded); err != nil {
		return nil, err
	}
	if uploaded.ID == "" {
		return nil, fmt.Errorf("upload response missing file id")
	}
	return &uploaded, nil
}

// ResolveInput maps a user-supplied input string to a FileRef without uploading.
// The caller is responsible for invoking UploadFile when LocalPath is non-empty.
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
		return FileRef{}, "", fmt.Errorf("input %q is not a local file, file_id, or URL: %w", input, statErr)
	}
}

func guessContentType(path string) string {
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

func jsonDecode(r io.Reader, v any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	return decodeJSON(body, v)
}

// ListFilesOptions is the wire shape for GET /files. The server's
// ListFilesParamsSchema accepts only nameContains/sortDir/paging — the
// schema's `sortBy` field is hardcoded to "createdAt" so the CLI doesn't
// expose it. (See the v2026-02-09 server schema, not the public OpenAPI
// doc, which is incomplete.)
type ListFilesOptions struct {
	NameContains string
	SortDir      string
	Limit        int
	PageToken    string
}

func (o ListFilesOptions) query() string {
	v := url.Values{}
	setIf(v, "nameContains", o.NameContains)
	setIf(v, "sortDir", o.SortDir)
	setIf(v, "nextPageToken", o.PageToken)
	if o.Limit > 0 {
		v.Set("maxPageSize", strconv.Itoa(o.Limit))
	}
	return encodeQuery(v)
}

// GetFileOptions controls which structured contents the server should
// include on the response. All three options are independent and may be
// combined; each populates a sibling field under `contents` on the response
// (rawText, markdown, html). When all three are false the response omits
// `contents` entirely.
type GetFileOptions struct {
	RawText  bool
	Markdown bool
	HTML     bool
}

func (c *Client) GetFile(ctx context.Context, id string) (*File, error) {
	return c.GetFileWithOptions(ctx, id, GetFileOptions{})
}

func (c *Client) GetFileWithOptions(ctx context.Context, id string, opts GetFileOptions) (*File, error) {
	var f File
	q := fileGetQuery(opts)
	if err := c.getJSON(ctx, "/files/"+id+q, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func fileGetQuery(opts GetFileOptions) string {
	parts := make([]string, 0, 3)
	if opts.RawText {
		parts = append(parts, "rawText=true")
	}
	if opts.Markdown {
		parts = append(parts, "markdown=true")
	}
	if opts.HTML {
		parts = append(parts, "html=true")
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

func (c *Client) ListFiles(ctx context.Context, opts ListFilesOptions) (*ListResponse[*File], error) {
	var out ListResponse[*File]
	if err := c.getJSON(ctx, "/files"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteFile(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/files/"+id, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) DownloadFile(ctx context.Context, id string, w io.Writer) (int64, error) {
	f, err := c.GetFile(ctx, id)
	if err != nil {
		return 0, err
	}
	if f.PresignedURL == "" {
		return 0, fmt.Errorf("file %s has no presigned URL (may have expired or be unavailable)", id)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.PresignedURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("download: storage returned %s", resp.Status)
	}
	return io.Copy(w, resp.Body)
}
