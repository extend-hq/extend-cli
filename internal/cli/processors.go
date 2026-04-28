package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

type processorAccessor[T any, V any] struct {
	noun        string
	pluralNoun  string
	exampleID   string
	rowFields   func(T) []string
	listFn      func(context.Context, *client.Client, client.ListProcessorsOptions) ([]T, string, error)
	getFn       func(context.Context, *client.Client, string) (T, error)
	listVerFn   func(context.Context, *client.Client, string) ([]V, error)
	getVerFn    func(context.Context, *client.Client, string, string) (V, error)
	verRowFn    func(V) []string
	createFn    func(context.Context, *client.Client, json.RawMessage) (T, error)
	updateFn    func(context.Context, *client.Client, string, json.RawMessage) (T, error)
	createVerFn func(context.Context, *client.Client, string, json.RawMessage) (V, error)
}

func (a processorAccessor[T, V]) cmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   a.pluralNoun,
		Short: fmt.Sprintf("List, inspect, and manage %s", a.pluralNoun),
	}
	cmd.AddCommand(a.listCmd(app))
	cmd.AddCommand(a.getCmd(app))
	if a.createFn != nil {
		cmd.AddCommand(a.createCmd(app))
	}
	if a.updateFn != nil {
		cmd.AddCommand(a.updateCmd(app))
	}
	cmd.AddCommand(a.versionsCmd(app))
	return cmd
}

func (a processorAccessor[T, V]) listCmd(app *App) *cobra.Command {
	var (
		limit   int
		all     bool
		sortDir string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List %s", a.pluralNoun),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListProcessorsOptions{
				Limit:   limit,
				SortBy:  "createdAt",
				SortDir: sortDir,
			}
			var rows [][]string
			var pages []any
			for {
				items, next, err := a.listFn(cmd.Context(), cli, opts)
				if err != nil {
					return err
				}
				pages = append(pages, items)
				for _, it := range items {
					rows = append(rows, a.rowFields(it))
				}
				if !all || next == "" {
					break
				}
				opts.PageToken = next
			}
			if app.Format != "" || !app.IO.IsStdoutTTY() {
				var raw any = pages
				if len(pages) == 1 {
					raw = pages[0]
				}
				return renderWithDefault(app, raw, output.FormatJSON)
			}
			if len(rows) == 0 {
				fmt.Fprintf(app.IO.Out, "No %s.\n", a.pluralNoun)
				return nil
			}
			return output.RenderTable(app.IO.Out, []string{"id", "name", "created"}, rows)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate to fetch every result")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	return cmd
}

func (a processorAccessor[T, V]) getCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("get <%s-id>", a.noun),
		Short: fmt.Sprintf("Show one %s by ID", a.noun),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			p, err := a.getFn(cmd.Context(), cli, args[0])
			if err != nil {
				return err
			}
			return renderWithDefault(app, p, output.FormatJSON)
		},
	}
	return cmd
}

func (a processorAccessor[T, V]) versionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions",
		Short: fmt.Sprintf("List or inspect versions of %s %s", articleFor(a.noun), a.noun),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   fmt.Sprintf("list <%s-id>", a.noun),
		Short: fmt.Sprintf("List versions of %s %s", articleFor(a.noun), a.noun),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			items, err := a.listVerFn(cmd.Context(), cli, args[0])
			if err != nil {
				return err
			}
			if app.Format != "" || !app.IO.IsStdoutTTY() {
				return renderWithDefault(app, items, output.FormatJSON)
			}
			rows := make([][]string, 0, len(items))
			for _, v := range items {
				rows = append(rows, a.verRowFn(v))
			}
			if len(rows) == 0 {
				fmt.Fprintln(app.IO.Out, "No versions.")
				return nil
			}
			return output.RenderTable(app.IO.Out, []string{"version", "id", "created"}, rows)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   fmt.Sprintf("get <%s-id> <version>", a.noun),
		Short: fmt.Sprintf("Show one %s version", a.noun),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			v, err := a.getVerFn(cmd.Context(), cli, args[0], args[1])
			if err != nil {
				return err
			}
			return renderWithDefault(app, v, output.FormatJSON)
		},
	})
	cmd.AddCommand(a.versionsCreateCmd(app))
	return cmd
}

func (a processorAccessor[T, V]) versionsCreateCmd(app *App) *cobra.Command {
	var fromFile, description string
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("create <%s-id>", a.noun),
		Short: fmt.Sprintf("Publish a new version of %s %s", articleFor(a.noun), a.noun),
		Args:  cobra.ExactArgs(1),
		Long: `Publish a new version. Pass --from-file body.json with the API body, or
use --description to publish the current draft with a note.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"description": description})
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			v, err := a.createVerFn(cmd.Context(), cli, args[0], body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, v, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON body for new version (- for stdin)")
	cmd.Flags().StringVar(&description, "description", "", "Description for the new version (overrides body)")
	return cmd
}

func (a processorAccessor[T, V]) createCmd(app *App) *cobra.Command {
	var fromFile, name, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: fmt.Sprintf("Create %s %s", articleFor(a.noun), a.noun),
		Long: fmt.Sprintf(`Create %s %s. Pass --from-file with the full API body, optionally
overlaying --name and --description from flags.`, articleFor(a.noun), a.noun),
		Example: fmt.Sprintf(`  extend %s create --from-file %s.json --name "My %s"
  cat %s.json | extend %s create --from-file -`, a.pluralNoun, a.noun, a.noun, a.noun, a.pluralNoun),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name, "description": description})
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			p, err := a.createFn(cmd.Context(), cli, body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, p, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON body (- for stdin)")
	cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
	cmd.Flags().StringVar(&description, "description", "", "Description (overrides body)")
	return cmd
}

func (a processorAccessor[T, V]) updateCmd(app *App) *cobra.Command {
	var fromFile, name, description string
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("update <%s-id>", a.noun),
		Short: fmt.Sprintf("Update an existing %s", a.noun),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name, "description": description})
			if err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			p, err := a.updateFn(cmd.Context(), cli, args[0], body)
			if err != nil {
				return err
			}
			return renderWithDefault(app, p, output.FormatJSON)
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to JSON patch body (- for stdin)")
	cmd.Flags().StringVar(&name, "name", "", "New name (overrides body)")
	cmd.Flags().StringVar(&description, "description", "", "New description (overrides body)")
	return cmd
}

// articleFor picks "a" or "an" based on the first letter only. Phonetic
// edge cases (silent h, X/M/etc. with vowel sound) are not handled because
// the only nouns this is called with are: extractor, classifier, splitter,
// workflow, evaluation. Don't trust this for arbitrary English.
func articleFor(noun string) string {
	if noun == "" {
		return "a"
	}
	switch noun[0] {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return "an"
	}
	return "a"
}

func mergeBody(fromFile string, overrides map[string]string) (json.RawMessage, error) {
	hasOverride := false
	for _, v := range overrides {
		if v != "" {
			hasOverride = true
			break
		}
	}

	if fromFile != "" && !hasOverride {
		raw, err := readBodyFile(fromFile)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			return json.RawMessage("{}"), nil
		}
		if !json.Valid(raw) {
			return nil, fmt.Errorf("parse --from-file: invalid JSON")
		}
		return raw, nil
	}

	data := map[string]any{}
	if fromFile != "" {
		raw, err := readBodyFile(fromFile)
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &data); err != nil {
				return nil, fmt.Errorf("parse --from-file: %w", err)
			}
		}
	}
	for k, v := range overrides {
		if v != "" {
			data[k] = v
		}
	}
	return json.Marshal(data)
}

const maxBodyFileBytes = 5 << 20

// readJSONFile is readBodyFile with a JSON syntax check. The error message
// names the flag for clarity ("--config: invalid JSON: ..."). Returns the
// raw bytes as json.RawMessage so callers can plug it directly into a
// struct field.
func readJSONFile(path, flag string) (json.RawMessage, error) {
	data, err := readBodyFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", flag, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("%s: not valid JSON", flag)
	}
	return data, nil
}

func readBodyFile(path string) ([]byte, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBodyFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBodyFileBytes {
		return nil, fmt.Errorf("body exceeded %d bytes", maxBodyFileBytes)
	}
	return data, nil
}

func extractorAccessor() processorAccessor[*client.Extractor, *client.ProcessorVersion] {
	return processorAccessor[*client.Extractor, *client.ProcessorVersion]{
		noun:       "extractor",
		pluralNoun: "extractors",
		rowFields:  func(e *client.Extractor) []string { return []string{e.ID, e.Name, relTime(e.CreatedAt)} },
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) ([]*client.Extractor, string, error) {
			r, err := c.ListExtractors(ctx, opts)
			if err != nil {
				return nil, "", err
			}
			return r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Extractor, error) {
			return c.GetExtractor(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string) ([]*client.ProcessorVersion, error) {
			r, err := c.ListExtractorVersions(ctx, id)
			if err != nil {
				return nil, err
			}
			return r.Data, nil
		},
		getVerFn: func(ctx context.Context, c *client.Client, id, ver string) (*client.ProcessorVersion, error) {
			return c.GetExtractorVersion(ctx, id, ver)
		},
		verRowFn: func(v *client.ProcessorVersion) []string { return []string{v.Version, v.ID, relTime(v.CreatedAt)} },
		createFn: func(ctx context.Context, c *client.Client, body json.RawMessage) (*client.Extractor, error) {
			return c.CreateExtractor(ctx, body)
		},
		updateFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.Extractor, error) {
			return c.UpdateExtractor(ctx, id, body)
		},
		createVerFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.ProcessorVersion, error) {
			return c.CreateExtractorVersion(ctx, id, body)
		},
	}
}

func classifierAccessor() processorAccessor[*client.Classifier, *client.ProcessorVersion] {
	return processorAccessor[*client.Classifier, *client.ProcessorVersion]{
		noun:       "classifier",
		pluralNoun: "classifiers",
		rowFields:  func(c *client.Classifier) []string { return []string{c.ID, c.Name, relTime(c.CreatedAt)} },
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) ([]*client.Classifier, string, error) {
			r, err := c.ListClassifiers(ctx, opts)
			if err != nil {
				return nil, "", err
			}
			return r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Classifier, error) {
			return c.GetClassifier(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string) ([]*client.ProcessorVersion, error) {
			r, err := c.ListClassifierVersions(ctx, id)
			if err != nil {
				return nil, err
			}
			return r.Data, nil
		},
		getVerFn: func(ctx context.Context, c *client.Client, id, ver string) (*client.ProcessorVersion, error) {
			return c.GetClassifierVersion(ctx, id, ver)
		},
		verRowFn: func(v *client.ProcessorVersion) []string { return []string{v.Version, v.ID, relTime(v.CreatedAt)} },
		createFn: func(ctx context.Context, c *client.Client, body json.RawMessage) (*client.Classifier, error) {
			return c.CreateClassifier(ctx, body)
		},
		updateFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.Classifier, error) {
			return c.UpdateClassifier(ctx, id, body)
		},
		createVerFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.ProcessorVersion, error) {
			return c.CreateClassifierVersion(ctx, id, body)
		},
	}
}

func splitterAccessor() processorAccessor[*client.Splitter, *client.ProcessorVersion] {
	return processorAccessor[*client.Splitter, *client.ProcessorVersion]{
		noun:       "splitter",
		pluralNoun: "splitters",
		rowFields:  func(s *client.Splitter) []string { return []string{s.ID, s.Name, relTime(s.CreatedAt)} },
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) ([]*client.Splitter, string, error) {
			r, err := c.ListSplitters(ctx, opts)
			if err != nil {
				return nil, "", err
			}
			return r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Splitter, error) {
			return c.GetSplitter(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string) ([]*client.ProcessorVersion, error) {
			r, err := c.ListSplitterVersions(ctx, id)
			if err != nil {
				return nil, err
			}
			return r.Data, nil
		},
		getVerFn: func(ctx context.Context, c *client.Client, id, ver string) (*client.ProcessorVersion, error) {
			return c.GetSplitterVersion(ctx, id, ver)
		},
		verRowFn: func(v *client.ProcessorVersion) []string { return []string{v.Version, v.ID, relTime(v.CreatedAt)} },
		createFn: func(ctx context.Context, c *client.Client, body json.RawMessage) (*client.Splitter, error) {
			return c.CreateSplitter(ctx, body)
		},
		updateFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.Splitter, error) {
			return c.UpdateSplitter(ctx, id, body)
		},
		createVerFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.ProcessorVersion, error) {
			return c.CreateSplitterVersion(ctx, id, body)
		},
	}
}

func workflowAccessor() processorAccessor[*client.Workflow, *client.ProcessorVersion] {
	return processorAccessor[*client.Workflow, *client.ProcessorVersion]{
		noun:       "workflow",
		pluralNoun: "workflows",
		rowFields:  func(w *client.Workflow) []string { return []string{w.ID, w.Name, relTime(w.CreatedAt)} },
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) ([]*client.Workflow, string, error) {
			r, err := c.ListWorkflows(ctx, opts)
			if err != nil {
				return nil, "", err
			}
			return r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Workflow, error) {
			return c.GetWorkflow(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string) ([]*client.ProcessorVersion, error) {
			r, err := c.ListWorkflowVersions(ctx, id)
			if err != nil {
				return nil, err
			}
			return r.Data, nil
		},
		getVerFn: func(ctx context.Context, c *client.Client, id, ver string) (*client.ProcessorVersion, error) {
			return c.GetWorkflowVersion(ctx, id, ver)
		},
		verRowFn: func(v *client.ProcessorVersion) []string { return []string{v.Version, v.ID, relTime(v.CreatedAt)} },
		createFn: func(ctx context.Context, c *client.Client, body json.RawMessage) (*client.Workflow, error) {
			return c.CreateWorkflow(ctx, body)
		},
		createVerFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.ProcessorVersion, error) {
			return c.CreateWorkflowVersion(ctx, id, body)
		},
	}
}
