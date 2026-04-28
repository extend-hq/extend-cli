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

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

func newFilesCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "Manage uploaded files",
	}
	cmd.AddCommand(newFilesUploadCommand(app))
	cmd.AddCommand(newFilesListCommand(app))
	cmd.AddCommand(newFilesGetCommand(app))
	cmd.AddCommand(newFilesDeleteCommand(app))
	cmd.AddCommand(newFilesDownloadCommand(app))
	return cmd
}

func newFilesUploadCommand(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "Upload a local file and print its file_id",
		Args:  cobra.ExactArgs(1),
		Example: `  extend files upload invoice.pdf
  extend files upload doc.pdf -o id`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			f, err := cli.UploadFile(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("upload: %w", err)
			}
			return renderWithDefault(app, f, output.FormatJSON)
		},
	}
	return cmd
}

func newFilesListCommand(app *App) *cobra.Command {
	var (
		nameContains string
		limit        int
		all          bool
		sortDir      string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List uploaded files",
		Example: `  extend files list
  extend files list --name-contains invoice --limit 50
  extend files list --all -o json --jq '.data[].id'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesList(cmd.Context(), app, nameContains, limit, all, sortDir)
		},
	}
	cmd.Flags().StringVar(&nameContains, "name-contains", "", "Filter to files whose name contains this substring")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum files to return per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate to fetch every file")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc (by createdAt)")
	return cmd
}

func runFilesList(ctx context.Context, app *App, nameContains string, limit int, all bool, sortDir string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	opts := client.ListFilesOptions{
		NameContains: nameContains,
		SortDir:      sortDir,
		Limit:        limit,
	}
	var rows [][]string
	var pages []any
	for {
		page, err := cli.ListFiles(ctx, opts)
		if err != nil {
			return err
		}
		pages = append(pages, page)
		for _, f := range page.Data {
			rows = append(rows, []string{
				f.ID,
				truncate(f.Name, 40),
				f.Type,
				relTime(f.CreatedAt),
			})
		}
		if !all || page.NextPageToken == "" {
			break
		}
		opts.PageToken = page.NextPageToken
	}

	return renderList(app, pages, []string{"id", "name", "type", "created"}, rows, "No files.")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func newFilesGetCommand(app *App) *cobra.Command {
	var (
		rawText  bool
		markdown bool
		html     bool
	)
	cmd := &cobra.Command{
		Use:   "get <file-id>",
		Short: "Show metadata for a file (includes a presigned download URL)",
		Long: `Show metadata for a file. By default returns the file summary
(id, name, type, presignedUrl, metadata).

Pass --raw-text, --markdown, or --html to additionally request structured
content under the response's "contents" field. The flags may be combined.`,
		Example: `  extend files get file_xK9 --raw-text
  extend files get file_xK9 --markdown --html -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			f, err := cli.GetFileWithOptions(cmd.Context(), args[0], client.GetFileOptions{
				RawText:  rawText,
				Markdown: markdown,
				HTML:     html,
			})
			if err != nil {
				return err
			}
			return renderWithDefault(app, f, output.FormatJSON)
		},
	}
	cmd.Flags().BoolVar(&rawText, "raw-text", false, "Include raw text content under contents.rawText")
	cmd.Flags().BoolVar(&markdown, "markdown", false, "Include markdown content under contents.markdown / contents.pages[].markdown")
	cmd.Flags().BoolVar(&html, "html", false, "Include HTML content under contents.pages[].html")
	return cmd
}

func newFilesDeleteCommand(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <file-id>",
		Short: "Delete an uploaded file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesDelete(cmd.Context(), app, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
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
	if err := cli.DeleteFile(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(app.IO.Out, "%s Deleted %s\n", paletteFor(app.IO).Green("✓"), id)
	return nil
}

func newFilesDownloadCommand(app *App) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "download <file-id>",
		Short: "Download a file to local disk (or stdout with -o -)",
		Args:  cobra.ExactArgs(1),
		Long: `Download a previously uploaded file via its presigned URL.

By default, writes to a file in the current directory using the file's name.
Pass --output <path> to choose a path, or --output - to stream to stdout.`,
		Example: `  extend files download file_xK9 --output invoice.pdf
  extend files download file_xK9 --output - | wc -c`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFilesDownload(cmd.Context(), app, args[0], outPath)
		},
	}
	cmd.Flags().StringVarP(&outPath, "output-file", "O", "", "Output path (defaults to file's name; '-' for stdout)")
	return cmd
}

func runFilesDownload(ctx context.Context, app *App, id, outPath string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	if outPath == "-" {
		_, err := cli.DownloadFile(ctx, id, app.IO.Out)
		return err
	}
	if outPath == "" {
		f, err := cli.GetFile(ctx, id)
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
	n, err := cli.DownloadFile(ctx, id, tmpFile)
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
