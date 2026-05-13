package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
)

// newDownloadDoc returns the typed documentation for `extend download`,
// the top-level convenience command that fetches file artifacts produced
// by a run (or referenced by file ID) without making the caller pluck
// IDs out of the run JSON themselves.
//
// Dispatch is keyed on the ID prefix:
//
//	file_*           the file itself
//	edr_*            the filled PDF emitted by an edit run
//	splr_*           every split file produced by a split run
//	workflow_run_*   every file output across the run's step runs
//
// Runs whose output is JSON (parse, extract, classify) deliberately
// error out with a pointer to `extend runs get` — there is nothing to
// "download" in those cases, only structured data to render.
func newDownloadDoc(app *App) *CommandDoc {
	var (
		outputDir  string
		outputFile string
	)
	return &CommandDoc{
		Use:     "download <id>",
		Summary: "Download file artifacts produced by a run, or fetch a file by ID",
		Group:   "Inspection",
		Triggers: []string{
			"download the filled pdf from an edit run",
			"save the split files produced by a splitter run",
			"fetch all output files from a workflow run",
			"download an uploaded file by its file_xxx id",
			"get edr/splr/workflow run artifacts to local disk",
		},
		WhenToUse: `Use to fetch file artifacts produced by Extend runs (edit, split,
workflow) or referenced by file ID. The ID prefix selects the source;
the command auto-walks the run record for downloadable files.

For runs whose output is structured JSON (parse, extract, classify),
use 'extend runs get <id>' instead — there is no file to download in
those cases.`,
		Details: `Resolves <id> based on its prefix:

  file_*           the file itself
  edr_*            the filled PDF emitted by an edit run
  splr_*           every split file (one per segment) produced by a split run
  workflow_run_*   every file output across the run's step runs

Single-file sources (file_*, edr_*) accept --output-file to choose the
exact path or '-' to stream raw bytes to stdout. Multi-file sources
(splr_*, workflow_run_*) require --output-dir (defaults to cwd); each
file lands under its server-assigned name. Identically-named files in
a multi-file download are disambiguated by appending '-2', '-3', etc.

Bytes-on-stdout (-O -) is reserved for single-file sources because the
caller has no way to disambiguate concatenated outputs.`,
		Examples: []Example{
			{Label: "File by ID, default to cwd", Cmd: "extend download file_xK9"},
			{Label: "File by ID, stream to stdout", Cmd: "extend download file_xK9 -O -"},
			{Label: "Edit run, write filled PDF to a specific path", Cmd: "extend download edr_xK9 --output-file filled.pdf"},
			{Label: "Split run, write all parts under ./splits", Cmd: "extend download splr_xK9 --output-dir ./splits"},
			{Label: "Workflow run, write all file outputs to ./out", Cmd: "extend download workflow_run_xK9 --output-dir ./out"},
		},
		Gotchas: []string{
			"Parse, extract, and classify runs have no file artifact; use 'extend runs get <id>' for their JSON output.",
			"--output-file is rejected for multi-file sources (split or workflow runs); use --output-dir instead.",
			"Presigned URLs expire after one hour; downloading long after the run completed may fail with a 'no presigned URL' error.",
			"Identically-named files in a multi-file download get '-2', '-3', etc. appended before the extension.",
		},
		SeeAlso: []string{"files download", "files get", "runs get"},
		Output:  OutputSpec{TTY: OutputBinary, Pipe: OutputBinary},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(cmd.Context(), app, args[0], outputDir, outputFile)
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVarP(&outputDir, "output-dir", "d", "", "Directory to write files to (default: current dir; created if missing)")
			cmd.Flags().StringVarP(&outputFile, "output-file", "O", "", "Single-file output path; '-' for stdout. Only valid for single-file sources.")
		},
	}
}

func runDownload(ctx context.Context, app *App, id, outputDir, outputFile string) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}
	fileIDs, err := resolveDownloadTargets(ctx, cli, id)
	if err != nil {
		return err
	}
	if len(fileIDs) == 0 {
		return fmt.Errorf("%s produced no downloadable file artifacts", id)
	}
	if outputFile != "" && len(fileIDs) > 1 {
		return fmt.Errorf("--output-file requires a single-file source; %s has %d (use --output-dir)", id, len(fileIDs))
	}
	if len(fileIDs) == 1 {
		return downloadSingleFile(ctx, app, cli, fileIDs[0], outputDir, outputFile)
	}
	return downloadMultipleFiles(ctx, app, cli, fileIDs, outputDir)
}

// resolveDownloadTargets returns the file IDs to download for the given
// source ID. It makes at most one network call (to fetch the parent run)
// and never to /files/ itself — that happens during the actual download.
func resolveDownloadTargets(ctx context.Context, cli *client.Client, id string) ([]string, error) {
	switch {
	case strings.HasPrefix(id, "file_"):
		return []string{id}, nil
	case strings.HasPrefix(id, "edr_"):
		run, err := cli.GetEditRun(ctx, id)
		if err != nil {
			return nil, err
		}
		if run.Output == nil || run.Output.EditedFile == nil || run.Output.EditedFile.ID == "" {
			return nil, fmt.Errorf("edit run %s has no editedFile output (status: %s)", id, run.Status)
		}
		return []string{run.Output.EditedFile.ID}, nil
	case strings.HasPrefix(id, "splr_"):
		run, err := cli.GetSplitRun(ctx, id)
		if err != nil {
			return nil, err
		}
		if run.Output == nil {
			return nil, fmt.Errorf("split run %s has no output (status: %s)", id, run.Status)
		}
		var ids []string
		for _, s := range run.Output.Splits {
			if s.FileID != "" {
				ids = append(ids, s.FileID)
			}
		}
		return ids, nil
	case strings.HasPrefix(id, "workflow_run_"):
		run, err := cli.GetWorkflowRun(ctx, id)
		if err != nil {
			return nil, err
		}
		return collectWorkflowFiles(run), nil
	case strings.HasPrefix(id, "exr_"),
		strings.HasPrefix(id, "pr_"),
		strings.HasPrefix(id, "clr_"):
		return nil, fmt.Errorf("%s runs produce JSON, not files; use 'extend runs get %s'", runTypeNameFromID(id), id)
	default:
		return nil, fmt.Errorf("unrecognized ID prefix for download: %s (expected file_, edr_, splr_, or workflow_run_)", id)
	}
}

// collectWorkflowFiles walks the step runs for any file outputs. The
// API surfaces per-step output files on StepRun.Files, so we don't need
// to decode each step's typed Result here. Files are deduplicated by ID
// in case the same file appears in multiple steps.
func collectWorkflowFiles(run *client.WorkflowRun) []string {
	seen := map[string]bool{}
	var out []string
	for _, step := range run.StepRuns {
		for _, f := range step.Files {
			if f.ID == "" || seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			out = append(out, f.ID)
		}
	}
	return out
}

func downloadSingleFile(ctx context.Context, app *App, cli *client.Client, fileID, outputDir, outputFile string) error {
	if outputFile == "-" {
		_, err := cli.DownloadFile(ctx, fileID, app.IO.Out)
		return err
	}
	path := outputFile
	if path == "" {
		f, err := cli.GetFile(ctx, fileID)
		if err != nil {
			return err
		}
		name := f.Name
		if name == "" {
			name = fileID + ".bin"
		}
		dir := outputDir
		if dir == "" {
			dir = "."
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path = filepath.Join(dir, name)
	} else if outputDir != "" {
		return fmt.Errorf("pass either --output-file or --output-dir, not both")
	}
	return writeFileFromExtend(ctx, app, cli, fileID, path)
}

func downloadMultipleFiles(ctx context.Context, app *App, cli *client.Client, fileIDs []string, outputDir string) error {
	dir := outputDir
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	used := map[string]bool{}
	for _, fid := range fileIDs {
		f, err := cli.GetFile(ctx, fid)
		if err != nil {
			return fmt.Errorf("get %s: %w", fid, err)
		}
		name := f.Name
		if name == "" {
			name = fid + ".bin"
		}
		out := uniqueName(name, used)
		used[out] = true
		path := filepath.Join(dir, out)
		if err := writeFileFromExtend(ctx, app, cli, fid, path); err != nil {
			return fmt.Errorf("download %s: %w", fid, err)
		}
	}
	return nil
}

// writeFileFromExtend streams a file's bytes to outPath atomically via
// a temp file in the same directory + rename. Matches the pattern used
// by `extend edit --output-file`.
func writeFileFromExtend(ctx context.Context, app *App, cli *client.Client, fileID, outPath string) error {
	dir := filepath.Dir(outPath)
	tmp, err := os.CreateTemp(dir, ".extend-dl-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := cli.DownloadFile(ctx, fileID, tmp)
	tmp.Close()
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, outPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	fmt.Fprintf(app.IO.ErrOut, "Wrote %d bytes to %s\n", n, outPath)
	return nil
}

// uniqueName produces a non-colliding filename: if `name` isn't already
// in `used`, returns name. Otherwise appends "-2", "-3", ... before the
// extension until a free slot is found.
func uniqueName(name string, used map[string]bool) string {
	if !used[name] {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if !used[candidate] {
			return candidate
		}
	}
}

// runTypeNameFromID maps an ID prefix to a human-readable run-type
// name for error messages.
func runTypeNameFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "exr_"):
		return "extract"
	case strings.HasPrefix(id, "pr_"):
		return "parse"
	case strings.HasPrefix(id, "clr_"):
		return "classify"
	}
	return "this kind of"
}
