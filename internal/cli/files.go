package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newFilesDoc returns the typed documentation for the `extend files` group
// and its five leaves: upload, list, get, delete, download.
func newFilesDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "files",
		Summary: "Manage uploaded files",
		Group:   "Inspection",
		WhenToUse: `Use these commands to upload, list, inspect, download, and delete files
in the workspace's storage. The action verbs (extract, classify, parse,
split, edit, run) auto-upload local paths so direct use is only needed
when scripting against the API or reusing a single upload across runs.`,
		Details: `Files are scoped to the current workspace; org-scoped API keys must pass
--workspace or set EXTEND_WORKSPACE_ID. Uploaded files persist until
explicitly deleted; runs continue to reference the file even if the user
later deletes it (run records remain, but the file content is gone).`,
		Subcommands: []*CommandDoc{
			newFilesUploadDoc(app),
			newFilesListDoc(app),
			newFilesGetDoc(app),
			newFilesDeleteDoc(app),
			newFilesDownloadDoc(app),
		},
	}
}

// newFilesUploadDoc returns the typed documentation for `extend files upload`.
func newFilesUploadDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "upload <path>",
		Summary: "Upload a local file and print its file_id",
		Triggers: []string{
			"upload a file to extend storage",
			"get a file_id for a local pdf",
			"persist a document for use across multiple runs",
			"pre-upload a file before extracting",
		},
		WhenToUse: `Use when you want to upload once and reference the file_id from multiple
subsequent runs, or when scripting against the API directly. The action
verbs (extract, classify, parse, split, edit, run) auto-upload local paths,
so direct upload is only required for these reuse and scripting cases.`,
		Details: `Upload a local file to Extend's storage and return the file metadata,
including the file_id used by subsequent runs.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend files upload invoice.pdf"},
			{Label: "ID-only output", Cmd: "extend files upload doc.pdf -o id"},
			{Label: "Capture and reuse", Cmd: `FID=$(extend files upload doc.pdf -o id) && extend extract "$FID" --using ex_abc`},
		},
		Gotchas: []string{
			"Upload size is bounded by the API; very large files may exceed limits.",
			"Files persist until explicitly deleted; clean up in long-running pipelines.",
		},
		SeeAlso: []string{"files list", "files get", "extract", "parse"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			f, err := extendx.UploadFile(cmd.Context(), cli, args[0])
			if err != nil {
				return fmt.Errorf("upload: %w", err)
			}
			return renderWithDefault(app, f, output.FormatJSON)
		},
	}
}

// newFilesListDoc returns the typed documentation for `extend files list`.
func newFilesListDoc(app *App) *CommandDoc {
	var (
		nameContains string
		limit        int
		maxN         int
		all          bool
		pageToken    string
		sortDir      string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: "List uploaded files",
		Triggers: []string{
			"list uploaded files in the workspace",
			"find a previously uploaded file id",
			"page through stored documents",
			"search files by name substring",
		},
		WhenToUse: `Use to discover file_ids of previously uploaded files. Filter by
--name-contains for substring matching on the original filename. Page
with --page-token; avoid --all in agent contexts because it auto-paginates.`,
		Details: `By default returns the first --limit (default 20) files; advance pages by
passing the response's nextPageToken to --page-token.

` + paginationGuidance,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend files list"},
			{Label: "Filter by name", Cmd: "extend files list --name-contains invoice --limit 50"},
			{Label: "Next page", Cmd: "extend files list --page-token <token-from-previous-response>"},
			{Label: "Just IDs", Cmd: "extend files list -o json --jq '.data[].id'"},
		},
		SeeAlso: []string{"files upload", "files get", "files delete"},
		Output:  OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesList(cmd, app, nameContains, limit, maxN, all, pageToken, sortDir)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&nameContains, "name-contains", "", "Filter to files whose name contains this substring")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc (by createdAt)")
		},
	}
}

func runFilesList(cmd *cobra.Command, app *App, nameContains string, limit, max int, all bool, pageToken, sortDir string) error {
	ctx := cmd.Context()
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	req := &extend.FilesListRequest{}
	if nameContains != "" {
		req.NameContains = extend.String(nameContains)
	}
	if sortDir != "" {
		sd, err := extend.NewSortDirFromString(sortDir)
		if err != nil {
			return fmt.Errorf("--sort: %w", err)
		}
		req.SortDir = &sd
	}
	if limit > 0 {
		ps := extend.MaxPageSize(limit)
		req.MaxPageSize = &ps
	}
	if pageToken != "" {
		req.NextPageToken = extend.String(pageToken)
	}

	var rows [][]string
	var pages []any
	for {
		page, err := cli.Files.List(ctx, req)
		if err != nil {
			return err
		}
		pages = append(pages, page)
		for _, f := range page.Data {
			rows = append(rows, []string{
				f.ID,
				truncate(f.Name, 40),
				string(extendx.Deref(f.Type)),
				relTime(f.CreatedAt),
			})
		}
		next := extendx.Deref(page.NextPageToken)
		if paginationDone(all, max, len(rows), next) {
			break
		}
		req.NextPageToken = extend.String(next)
	}
	rows = capRowsToMax(rows, max)

	return renderListForCmd(cmd, app, pages, []string{"id", "name", "type", "created"}, rows, "No files.")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}



// newFilesGetDoc returns the typed documentation for `extend files get`.
func newFilesGetDoc(app *App) *CommandDoc {
	var (
		rawText  bool
		markdown bool
		html     bool
	)
	return &CommandDoc{
		Use:     "get <file-id>",
		Summary: "Show metadata for a file (with presigned download URL)",
		Triggers: []string{
			"get metadata for an uploaded file",
			"fetch the presigned url for a file",
			"retrieve raw text content of a stored file",
			"inspect a previously uploaded document",
		},
		WhenToUse: `Use to retrieve the file metadata, presigned download URL, or
optionally raw text/markdown/html contents of a previously uploaded file.`,
		Details: `By default returns the file summary (id, name, type, presignedUrl,
metadata).

Pass --raw-text, --markdown, or --html to additionally request structured
content under the response's "contents" field. The flags may be combined.`,
		Examples: []Example{
			{Label: "Get raw text", Cmd: "extend files get file_xK9 --raw-text"},
			{Label: "Markdown and HTML", Cmd: "extend files get file_xK9 --markdown --html -o json"},
		},
		Gotchas: []string{
			"Content flags trigger server-side processing and may take longer than the bare metadata fetch.",
		},
		SeeAlso: []string{"files list", "files download", "files delete"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			req := &extend.FilesRetrieveRequest{}
			if rawText {
				req.RawText = extend.Bool(true)
			}
			if markdown {
				req.Markdown = extend.Bool(true)
			}
			if html {
				req.HTML = extend.Bool(true)
			}
			f, err := cli.Files.Retrieve(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			return renderWithDefault(app, f, output.FormatJSON)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&rawText, "raw-text", false, "Include raw text content under contents.rawText")
			cmd.Flags().BoolVar(&markdown, "markdown", false, "Include markdown content under contents.markdown / contents.pages[].markdown")
			cmd.Flags().BoolVar(&html, "html", false, "Include HTML content under contents.pages[].html")
		},
	}
}

// newFilesDeleteDoc returns the typed documentation for `extend files delete`.
func newFilesDeleteDoc(app *App) *CommandDoc {
	var yes bool
	return &CommandDoc{
		Use:     "delete <file-id>",
		Summary: "Delete an uploaded file",
		Triggers: []string{
			"delete an uploaded file",
			"remove a previously stored document",
			"clean up files in extend storage",
		},
		WhenToUse: `Use to permanently remove an uploaded file from storage. Existing run
records that reference the file are not affected, but the file's content
can no longer be retrieved via 'extend files download'.`,
		Details: `Prompts for confirmation when stdin is a TTY; pass --yes to skip the
prompt (required in non-interactive scripts).`,
		Examples: []Example{
			{Label: "With prompt", Cmd: "extend files delete file_xK9"},
			{Label: "Skip confirmation", Cmd: "extend files delete file_xK9 --yes"},
		},
		Gotchas: []string{
			"Deletion is permanent; the file content cannot be recovered.",
			"Without --yes in non-TTY contexts, the command refuses to delete.",
		},
		SeeAlso: []string{"files list", "files get"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesDelete(cmd.Context(), app, args[0], yes)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
		},
	}
}

func runFilesDelete(ctx context.Context, app *App, id string, yes bool) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	if !yes {
		if !app.IO.IsStdinTTY() {
			return errors.New("refusing to delete without confirmation; pass --yes to skip prompt in non-interactive contexts")
		}
		fmt.Fprintf(app.IO.ErrOut, "Delete file %s? [y/N]: ", id)
		reader := bufio.NewReader(app.IO.In)
		line, _ := reader.ReadString('\n')
		line = strings.ToLower(strings.TrimSpace(line))
		if line != "y" && line != "yes" {
			fmt.Fprintln(app.IO.ErrOut, "Aborted.")
			return nil
		}
	}
	if _, err := cli.Files.Delete(ctx, id, &extend.FilesDeleteRequest{}); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.ErrOut, "%s Deleted %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}

// newFilesDownloadDoc returns the typed documentation for `extend files download`.
func newFilesDownloadDoc(app *App) *CommandDoc {
	var outPath string
	return &CommandDoc{
		Use:     "download <file-id>",
		Summary: "Download a file to local disk (or stdout with -O -)",
		Triggers: []string{
			"download a file from extend storage",
			"fetch a previously uploaded pdf",
			"retrieve the bytes of a stored file",
			"save an uploaded file to local disk",
		},
		WhenToUse: `Use to retrieve the bytes of a previously uploaded file by ID. Combine
with -O - to stream to stdout for use in pipelines.`,
		Details: `Download a previously uploaded file via its presigned URL.

By default, writes to a file in the current directory using the file's name.
Pass --output-file <path> to choose a path, or --output-file - to stream to
stdout.`,
		Examples: []Example{
			{Label: "Save with name", Cmd: "extend files download file_xK9 --output-file invoice.pdf"},
			{Label: "Stream to pipe", Cmd: "extend files download file_xK9 -O - | wc -c"},
		},
		Gotchas: []string{
			"Without --output-file, the command writes to the current directory using the file's stored name.",
			"Use --output-file - to stream to stdout (no terminal heuristics).",
		},
		SeeAlso: []string{"files get", "files list"},
		Output:  OutputSpec{TTY: OutputBinary, Pipe: OutputBinary},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesDownload(cmd.Context(), app, args[0], outPath)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVarP(&outPath, "output-file", "O", "", "Output path (defaults to file's name; '-' for stdout)")
		},
	}
}

func runFilesDownload(ctx context.Context, app *App, id, outPath string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	if outPath == "-" {
		_, err := extendx.DownloadFile(ctx, cli, id, app.IO.Out)
		return err
	}
	if outPath == "" {
		f, err := cli.Files.Retrieve(ctx, id, &extend.FilesRetrieveRequest{})
		if err != nil {
			return err
		}
		if f.Name != "" {
			outPath = filepath.Base(f.Name)
		} else {
			outPath = id
		}
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(outPath), ".extend-dl-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)
	n, err := extendx.DownloadFile(ctx, cli, id, tmpFile)
	tmpFile.Close()
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Fprintf(app.IO.ErrOut, "Wrote %d bytes to %s\n", n, outPath)
	return nil
}
