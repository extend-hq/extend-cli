package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

type processorAccessor[T any, V any] struct {
	noun        string
	pluralNoun  string
	exampleID   string
	rowFields   func(T) []string
	listFn      func(context.Context, *client.Client, client.ListProcessorsOptions) (any, []T, string, error)
	getFn       func(context.Context, *client.Client, string) (T, error)
	listVerFn   func(context.Context, *client.Client, string, client.ListProcessorVersionsOptions) (any, []V, string, error)
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
		sortBy  string
		sortDir string
		limit   int
		all     bool
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
				SortBy:  sortBy,
				SortDir: sortDir,
			}
			var rows [][]string
			var pages []any
			for {
				page, items, next, err := a.listFn(cmd.Context(), cli, opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				for _, it := range items {
					rows = append(rows, a.rowFields(it))
				}
				if !all || next == "" {
					break
				}
				opts.PageToken = next
			}
			return renderList(app, pages, []string{"id", "name", "created"}, rows,
				fmt.Sprintf("No %s.", a.pluralNoun))
		},
	}
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
	cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results per page")
	cmd.Flags().BoolVar(&all, "all", false, "Auto-paginate to fetch every result")
	SetIOAnnotations(cmd, OutputTable, OutputJSON)
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
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func (a processorAccessor[T, V]) versionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versions",
		Short: fmt.Sprintf("List or inspect versions of %s %s", articleFor(a.noun), a.noun),
	}
	var (
		verSortDir string
		verLimit   int
		verAll     bool
	)
	listCmd := &cobra.Command{
		Use:   fmt.Sprintf("list <%s-id>", a.noun),
		Short: fmt.Sprintf("List versions of %s %s", articleFor(a.noun), a.noun),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := client.ListProcessorVersionsOptions{
				SortDir: verSortDir,
				Limit:   verLimit,
			}
			var allItems []V
			var pages []any
			for {
				page, items, next, err := a.listVerFn(cmd.Context(), cli, args[0], opts)
				if err != nil {
					return err
				}
				pages = append(pages, page)
				allItems = append(allItems, items...)
				if !verAll || next == "" {
					break
				}
				opts.PageToken = next
			}
			rows := make([][]string, 0, len(allItems))
			for _, v := range allItems {
				rows = append(rows, a.verRowFn(v))
			}
			return renderList(app, pages, []string{"version", "id", "created"}, rows, "No versions.")
		},
	}
	listCmd.Flags().StringVar(&verSortDir, "sort", "desc", "Sort direction: asc|desc")
	listCmd.Flags().IntVar(&verLimit, "limit", 20, "Maximum versions per page")
	listCmd.Flags().BoolVar(&verAll, "all", false, "Auto-paginate to fetch every version")
	SetIOAnnotations(listCmd, OutputTable, OutputJSON)
	cmd.AddCommand(listCmd)
	getVerCmd := &cobra.Command{
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
	}
	SetIOAnnotations(getVerCmd, OutputJSON, OutputJSON)
	cmd.AddCommand(getVerCmd)
	cmd.AddCommand(a.versionsCreateCmd(app))
	return cmd
}

func (a processorAccessor[T, V]) versionsCreateCmd(app *App) *cobra.Command {
	var fromFile, description, releaseType, name string
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("create <%s-id>", a.noun),
		Short: fmt.Sprintf("Publish a new version of %s %s", articleFor(a.noun), a.noun),
		Args:  cobra.ExactArgs(1),
		Long:  versionCreateLong(a.noun),
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides := map[string]string{}
			if a.noun == "workflow" {
				overrides["name"] = name
			} else {
				overrides["description"] = description
				overrides["releaseType"] = releaseType
			}
			body, err := mergeBody(fromFile, overrides)
			if err != nil {
				return err
			}
			if a.noun != "workflow" {
				if err := requireJSONEnum(body, "releaseType", "major", "minor"); err != nil {
					return err
				}
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
	if a.noun == "workflow" {
		cmd.Flags().StringVar(&name, "name", "", "Name for the deployed workflow version (overrides body)")
	} else {
		cmd.Flags().StringVar(&releaseType, "release-type", "", "Release type: major|minor (required unless provided by --from-file)")
		cmd.Flags().StringVar(&description, "description", "", "Description for the new version (overrides body)")
	}
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func versionCreateLong(noun string) string {
	if noun == "workflow" {
		return `Deploy a new workflow version. Pass --from-file with the API body
(inline JSON, path, file:// URI, or - for stdin), or use --name to name the
deployed version.`
	}
	return `Publish a new version. Pass --release-type major|minor, or provide the
full API body with --from-file (inline JSON, path, file:// URI, or - for stdin).
Use --description to publish the current draft with a note.`
}

func requireJSONEnum(body json.RawMessage, field string, allowed ...string) error {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return fmt.Errorf("parse body: %w", err)
	}
	got, ok := obj[field].(string)
	if !ok || got == "" {
		return fmt.Errorf("%s is required (pass --%s or include %q in --from-file)", field, flagName(field), field)
	}
	for _, want := range allowed {
		if got == want {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of: %s", field, strings.Join(allowed, "|"))
}

func flagName(field string) string {
	var out strings.Builder
	for i, r := range field {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('-')
		}
		out.WriteRune(r)
	}
	return strings.ToLower(out.String())
}

func (a processorAccessor[T, V]) createCmd(app *App) *cobra.Command {
	var fromFile, name string
	cmd := &cobra.Command{
		Use:   "create",
		Short: fmt.Sprintf("Create %s %s", articleFor(a.noun), a.noun),
		Long: fmt.Sprintf(`Create %s %s. Pass --from-file with the full API body
(inline JSON, path, file:// URI, or - for stdin), optionally overlaying --name
from flags.`, articleFor(a.noun), a.noun),
		Example: fmt.Sprintf(`  extend %s create --from-file %s.json --name "My %s"
  cat %s.json | extend %s create --from-file -`, a.pluralNoun, a.noun, a.noun, a.noun, a.pluralNoun),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name})
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
	return cmd
}

func (a processorAccessor[T, V]) updateCmd(app *App) *cobra.Command {
	var fromFile, name string
	cmd := &cobra.Command{
		Use:   fmt.Sprintf("update <%s-id>", a.noun),
		Short: fmt.Sprintf("Update an existing %s", a.noun),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := mergeBody(fromFile, map[string]string{"name": name})
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
	cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "New name (overrides body)")
	SetIOAnnotations(cmd, OutputJSON, OutputJSON)
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
		raw, err := readJSONFile(fromFile, "--from-file")
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			return json.RawMessage("{}"), nil
		}
		return raw, nil
	}

	data := map[string]any{}
	if fromFile != "" {
		raw, err := readJSONFile(fromFile, "--from-file")
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &data); err != nil {
				return nil, fmt.Errorf("--from-file: %w", err)
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

// readJSONFile accepts inline JSON, a path, stdin (-), or an absolute file://
// URI, then validates JSON syntax. The error message names the flag for
// clarity ("--config: invalid JSON: ..."). Returns the raw bytes as
// json.RawMessage so callers can plug it directly into a struct field.
func readJSONFile(path, flag string) (json.RawMessage, error) {
	data, err := readJSONSource(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", flag, err)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("%s: not valid JSON", flag)
	}
	return data, nil
}

func readJSONSource(source string) ([]byte, error) {
	trimmed := strings.TrimSpace(source)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return []byte(trimmed), nil
	}
	path, err := pathFromFileURI(source)
	if err != nil {
		return nil, err
	}
	if path != "" {
		return readBodyFile(path)
	}
	return readBodyFile(source)
}

func pathFromFileURI(source string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(source), "file://") {
		return "", nil
	}
	u, err := url.Parse(source)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", nil
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("file URI must not include query or fragment")
	}
	if u.Host != "" && u.Host != "localhost" {
		if runtime.GOOS != "windows" {
			return "", fmt.Errorf("file URI host %q is only supported for Windows UNC paths", u.Host)
		}
		return `\\` + u.Host + filepath.FromSlash(u.Path), nil
	}
	p := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && strings.HasPrefix(p, `\`) && len(p) >= 4 && p[2] == ':' {
		p = p[1:]
	}
	if p == "" {
		return "", fmt.Errorf("file URI must include a path")
	}
	return p, nil
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
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) (any, []*client.Extractor, string, error) {
			r, err := c.ListExtractors(ctx, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Extractor, error) {
			return c.GetExtractor(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string, opts client.ListProcessorVersionsOptions) (any, []*client.ProcessorVersion, string, error) {
			r, err := c.ListExtractorVersions(ctx, id, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
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
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) (any, []*client.Classifier, string, error) {
			r, err := c.ListClassifiers(ctx, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Classifier, error) {
			return c.GetClassifier(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string, opts client.ListProcessorVersionsOptions) (any, []*client.ProcessorVersion, string, error) {
			r, err := c.ListClassifierVersions(ctx, id, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
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
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) (any, []*client.Splitter, string, error) {
			r, err := c.ListSplitters(ctx, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Splitter, error) {
			return c.GetSplitter(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string, opts client.ListProcessorVersionsOptions) (any, []*client.ProcessorVersion, string, error) {
			r, err := c.ListSplitterVersions(ctx, id, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
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
		listFn: func(ctx context.Context, c *client.Client, opts client.ListProcessorsOptions) (any, []*client.Workflow, string, error) {
			r, err := c.ListWorkflows(ctx, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
		},
		getFn: func(ctx context.Context, c *client.Client, id string) (*client.Workflow, error) {
			return c.GetWorkflow(ctx, id)
		},
		listVerFn: func(ctx context.Context, c *client.Client, id string, opts client.ListProcessorVersionsOptions) (any, []*client.ProcessorVersion, string, error) {
			r, err := c.ListWorkflowVersions(ctx, id, opts)
			if err != nil {
				return nil, nil, "", err
			}
			return r, r.Data, r.NextPageToken, nil
		},
		getVerFn: func(ctx context.Context, c *client.Client, id, ver string) (*client.ProcessorVersion, error) {
			return c.GetWorkflowVersion(ctx, id, ver)
		},
		verRowFn: func(v *client.ProcessorVersion) []string { return []string{v.Version, v.ID, relTime(v.CreatedAt)} },
		createFn: func(ctx context.Context, c *client.Client, body json.RawMessage) (*client.Workflow, error) {
			return c.CreateWorkflow(ctx, body)
		},
		updateFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.Workflow, error) {
			return c.UpdateWorkflow(ctx, id, body)
		},
		createVerFn: func(ctx context.Context, c *client.Client, id string, body json.RawMessage) (*client.ProcessorVersion, error) {
			return c.CreateWorkflowVersion(ctx, id, body)
		},
	}
}
