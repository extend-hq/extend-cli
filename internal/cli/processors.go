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

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/output"
)

// processorAccessor parameterizes the shared
// list/get/create/update/versions structure for one resource family
// (extractors, classifiers, splitters, workflows). The shape varies
// in details — version create accepts different per-resource fields,
// versions are surfaced as different SDK types — so we plug each via
// function pointers.
//
// Type parameters (each unconstrained — Go generics require an
// explicit constraint, and `any` means "no constraint"):
//
//   - T  = full resource type (e.g. *extend.Extractor) returned by
//     get/create/update.
//   - TS = list-summary type (e.g. *extend.ExtractorSummary) used
//     for resource list rows.
//   - V  = full version type (e.g. *extend.ExtractorVersion)
//     returned by version get/create.
//   - VS = version summary type (e.g.
//     *extend.ExtractorVersionSummary) used in versions-list rows.
//
// The list callbacks return the SDK response object as the first
// value so the JSON-rendering pipeline (renderListForCmd) can emit
// it verbatim; that pipeline takes `[]any` because each family's
// response is a different concrete type, so the heterogeneity is
// unavoidable at that boundary.
type processorAccessor[T any, TS any, V any, VS any] struct {
	noun       string
	pluralNoun string
	exampleID  string
	// runVerb is the top-level action verb that uses this resource.
	// For extractors it's "extract"; for classifiers, "classify";
	// for splitters, "split"; for workflows, "run". Used to populate
	// SeeAlso references back to the action verb in the list doc.
	runVerb     string
	rowFields   func(TS) []string
	listFn      func(ctx context.Context, c *sdkclient.Client, opts listProcessorsOptions) (resp any, data []TS, nextPageToken string, err error)
	getFn       func(ctx context.Context, c *sdkclient.Client, id string) (T, error)
	listVerFn   func(ctx context.Context, c *sdkclient.Client, id string, opts listProcessorVersionsOptions) (resp any, data []VS, nextPageToken string, err error)
	getVerFn    func(ctx context.Context, c *sdkclient.Client, id, ver string) (V, error)
	verRowFn    func(VS) []string
	createFn    func(ctx context.Context, c *sdkclient.Client, body json.RawMessage) (T, error)
	updateFn    func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (T, error)
	createVerFn func(ctx context.Context, c *sdkclient.Client, id string, body json.RawMessage) (V, error)
}

// listProcessorsOptions is the CLI-side carrier for processor list
// flags. Each *Fn translates this into the SDK's per-resource list
// request struct.
type listProcessorsOptions struct {
	SortBy    string
	SortDir   string
	Limit     int
	PageToken string
}

// listProcessorVersionsOptions is the CLI-side carrier for version
// list flags.
type listProcessorVersionsOptions struct {
	SortDir   string
	Limit     int
	PageToken string
}

// doc returns the typed CommandDoc tree for one resource family
// (extractors / classifiers / splitters / workflows). The same
// structure — list, get, create, update, versions [list/get/create] —
// is generated for each, parameterised by noun.
func (a processorAccessor[T, TS, V, VS]) doc(app *App) *CommandDoc {
	subs := []*CommandDoc{
		a.listDoc(app),
		a.getDoc(app),
	}
	if a.createFn != nil {
		subs = append(subs, a.createDoc(app))
	}
	if a.updateFn != nil {
		subs = append(subs, a.updateDoc(app))
	}
	subs = append(subs, a.versionsDoc(app))
	return &CommandDoc{
		Use:     a.pluralNoun,
		Summary: fmt.Sprintf("List, inspect, and manage %s", a.pluralNoun),
		Group:   "Resources",
		WhenToUse: fmt.Sprintf(`Use these commands to discover, inspect, create, update, and version
%s in the workspace.`, a.pluralNoun),
		Details: fmt.Sprintf(`%s %s has a draft (the editable working copy) plus zero or more
published versions (immutable snapshots). The draft is what 'update'
changes; 'versions create' publishes the draft as a new immutable
version.`, capitalize(articleFor(a.noun)), a.noun),
		Subcommands: subs,
	}
}

func (a processorAccessor[T, TS, V, VS]) listDoc(app *App) *CommandDoc {
	var (
		sortBy    string
		sortDir   string
		limit     int
		maxN      int
		all       bool
		pageToken string
	)
	return &CommandDoc{
		Use:     "list",
		Summary: fmt.Sprintf("List %s", a.pluralNoun),
		Triggers: []string{
			fmt.Sprintf("list %s in the workspace", a.pluralNoun),
			fmt.Sprintf("page through %s", a.pluralNoun),
			fmt.Sprintf("find %s ids", a.pluralNoun),
			fmt.Sprintf("discover available %s", a.pluralNoun),
		},
		WhenToUse: fmt.Sprintf(`Use to discover %s by ID, optionally sorted by createdAt or updatedAt.
The ID column feeds 'extend %s get <id>'.`, a.pluralNoun, a.pluralNoun),
		Details: fmt.Sprintf(`Sorted by updatedAt descending by default. By default, returns the
first page (--limit, default 20). When more pages exist, the response's
nextPageToken (and a stderr hint on TTYs) tells you the token to pass
to --page-token to fetch the next page.

%s

The %s ID column is the input for `+"`extend %s get <id>`"+`.`,
			paginationGuidance, a.noun, a.pluralNoun),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s list", a.pluralNoun)},
			{Label: "Sort by createdAt", Cmd: fmt.Sprintf("extend %s list --sort-by createdAt", a.pluralNoun)},
			{Label: "Next page", Cmd: fmt.Sprintf("extend %s list --page-token <token-from-previous-response>", a.pluralNoun)},
			{Label: "First five IDs", Cmd: fmt.Sprintf("extend %s list -o id | head -5", a.pluralNoun)},
			{Label: "Just IDs via jq", Cmd: fmt.Sprintf("extend %s list --jq '.data[].id' -o raw", a.pluralNoun)},
		},
		SeeAlso: []string{
			fmt.Sprintf("%s get", a.pluralNoun),
			fmt.Sprintf("%s create", a.pluralNoun),
			a.runVerb,
		},
		Output: OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := listProcessorsOptions{
				Limit:     limit,
				SortBy:    sortBy,
				SortDir:   sortDir,
				PageToken: pageToken,
			}
			var rows [][]string
			var pages []any
			for {
				resp, items, next, err := a.listFn(cmd.Context(), cli, opts)
				if err != nil {
					return err
				}
				pages = append(pages, resp)
				for _, it := range items {
					rows = append(rows, a.rowFields(it))
				}
				if paginationDone(all, maxN, len(rows), next) {
					break
				}
				opts.PageToken = next
			}
			rows = capRowsToMax(rows, maxN)
			return renderListForCmd(cmd, app, pages, []string{"id", "name", "created"}, rows,
				fmt.Sprintf("No %s.", a.pluralNoun))
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: updatedAt|createdAt (server default: updatedAt)")
			cmd.Flags().StringVar(&sortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&limit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&maxN, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&pageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&all, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func (a processorAccessor[T, TS, V, VS]) getDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     fmt.Sprintf("get <%s-id>", a.noun),
		Summary: fmt.Sprintf("Show one %s by ID", a.noun),
		Triggers: []string{
			fmt.Sprintf("show one %s by id", a.noun),
			fmt.Sprintf("inspect a %s configuration", a.noun),
			fmt.Sprintf("get %s name and metadata", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to retrieve full details for one %s, including its current
draft and deployed-version metadata.`, a.noun),
		Details: fmt.Sprintf(`Use `+"`extend %s versions list <id>`"+` to enumerate historical versions, or
`+"`extend %s versions get <id> <version>`"+` to inspect a specific one.`,
			a.pluralNoun, a.pluralNoun),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s get %s", a.pluralNoun, a.exampleID)},
			{Label: "Just the name", Cmd: fmt.Sprintf("extend %s get %s --jq '.name' -o raw", a.pluralNoun, a.exampleID)},
		},
		SeeAlso: []string{
			fmt.Sprintf("%s list", a.pluralNoun),
			fmt.Sprintf("%s versions list", a.pluralNoun),
			fmt.Sprintf("%s update", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:   cobra.ExactArgs(1),
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
}

func (a processorAccessor[T, TS, V, VS]) versionsDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "versions",
		Summary: fmt.Sprintf("List or inspect versions of %s %s", articleFor(a.noun), a.noun),
		WhenToUse: fmt.Sprintf(`Use this group to enumerate published versions of %s %s, inspect a
specific version's config, or publish a new version from the current
draft.`, articleFor(a.noun), a.noun),
		Details: fmt.Sprintf(`Versions are immutable snapshots of %s %s's config. The draft is the
editable working copy and is not a version.`, articleFor(a.noun), a.noun),
		Subcommands: []*CommandDoc{
			a.versionsListDoc(app),
			a.versionsGetDoc(app),
			a.versionsCreateDoc(app),
		},
	}
}

func (a processorAccessor[T, TS, V, VS]) versionsListDoc(app *App) *CommandDoc {
	var (
		verSortDir   string
		verLimit     int
		verMax       int
		verAll       bool
		verPageToken string
	)
	return &CommandDoc{
		Use:     fmt.Sprintf("list <%s-id>", a.noun),
		Summary: fmt.Sprintf("List versions of %s %s", articleFor(a.noun), a.noun),
		Triggers: []string{
			fmt.Sprintf("list versions of a %s", a.noun),
			fmt.Sprintf("see published %s versions", a.noun),
			fmt.Sprintf("page through historical %s versions", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to enumerate every published version of %s %s. Sort defaults to
descending by createdAt (newest first).`, articleFor(a.noun), a.noun),
		Details: fmt.Sprintf(`Versions are immutable snapshots of a %s's config; the row labeled "draft"
is the editable working copy.

%s`, a.noun, paginationGuidance),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s versions list %s", a.pluralNoun, a.exampleID)},
			{Label: "Next page", Cmd: fmt.Sprintf("extend %s versions list %s --page-token <token-from-previous-response>", a.pluralNoun, a.exampleID)},
			{Label: "Just version numbers", Cmd: fmt.Sprintf("extend %s versions list %s --jq '.data[].version' -o raw", a.pluralNoun, a.exampleID)},
		},
		SeeAlso: []string{
			fmt.Sprintf("%s get", a.pluralNoun),
			fmt.Sprintf("%s versions get", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputTable, Pipe: OutputJSON},
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			opts := listProcessorVersionsOptions{
				SortDir:   verSortDir,
				Limit:     verLimit,
				PageToken: verPageToken,
			}
			var allItems []VS
			var pages []any
			for {
				resp, items, next, err := a.listVerFn(cmd.Context(), cli, args[0], opts)
				if err != nil {
					return err
				}
				pages = append(pages, resp)
				allItems = append(allItems, items...)
				if paginationDone(verAll, verMax, len(allItems), next) {
					break
				}
				opts.PageToken = next
			}
			allItems = capRowsToMax(allItems, verMax)
			rows := make([][]string, 0, len(allItems))
			for _, v := range allItems {
				rows = append(rows, a.verRowFn(v))
			}
			return renderListForCmd(cmd, app, pages, []string{"version", "id", "created"}, rows, "No versions.")
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&verSortDir, "sort", "desc", "Sort direction: asc|desc")
			cmd.Flags().IntVar(&verLimit, "limit", 20, "Page size used in each API request (advanced)")
			cmd.Flags().IntVar(&verMax, "max", 0, "Stop after at most N total results, auto-paginating internally (0 = single page)")
			cmd.Flags().StringVar(&verPageToken, "page-token", "", "Resume from a specific page (cursor from a previous response; advanced — prefer --max)")
			cmd.Flags().BoolVar(&verAll, "all", false, "Fetch every page (use --max for a bounded fetch)")
		},
	}
}

func (a processorAccessor[T, TS, V, VS]) versionsGetDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     fmt.Sprintf("get <%s-id> <version>", a.noun),
		Summary: fmt.Sprintf("Show one %s version", a.noun),
		Triggers: []string{
			fmt.Sprintf("show one %s version config", a.noun),
			fmt.Sprintf("inspect a specific %s version", a.noun),
			fmt.Sprintf("clone a %s version into a snapshot", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to retrieve the full config for a published %s version, or pass
"draft" as the version to inspect the working copy.`, a.noun),
		Details: fmt.Sprintf(`The output is the canonical JSON shape used by `+"`extend %s versions create --from-file`"+`,
so this command is also useful for cloning a known-good version into a
new one.`, a.pluralNoun),
		Examples: []Example{
			{Label: "Specific version", Cmd: fmt.Sprintf("extend %s versions get %s 1.0", a.pluralNoun, a.exampleID)},
			{Label: "Draft", Cmd: fmt.Sprintf("extend %s versions get %s draft", a.pluralNoun, a.exampleID)},
			{Label: "Snapshot to file", Cmd: fmt.Sprintf("extend %s versions get %s 1.0 > snapshot.json", a.pluralNoun, a.exampleID)},
		},
		SeeAlso: []string{
			fmt.Sprintf("%s versions list", a.pluralNoun),
			fmt.Sprintf("%s versions create", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:   cobra.ExactArgs(2),
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
}

func (a processorAccessor[T, TS, V, VS]) versionsCreateDoc(app *App) *CommandDoc {
	var fromFile, description, releaseType, name string
	return &CommandDoc{
		Use:     fmt.Sprintf("create <%s-id>", a.noun),
		Summary: fmt.Sprintf("Publish a new version of %s %s", articleFor(a.noun), a.noun),
		Triggers: []string{
			fmt.Sprintf("publish a new %s version", a.noun),
			fmt.Sprintf("deploy a %s draft as a version", a.noun),
			fmt.Sprintf("release a %s as major or minor", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to publish the current draft of %s %s as a new immutable version.
For workflows, this is a named deploy; for processors, a major/minor
release.`, articleFor(a.noun), a.noun),
		Details:  versionCreateLong(a.noun),
		Examples: versionCreateExamples(a.noun, a.pluralNoun, a.exampleID),
		Gotchas:  versionCreateGotchas(a.noun),
		SeeAlso: []string{
			fmt.Sprintf("%s update", a.pluralNoun),
			fmt.Sprintf("%s versions list", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:   cobra.ExactArgs(1),
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
			if a.noun == "workflow" {
				cmd.Flags().StringVar(&name, "name", "", "Name for the deployed workflow version (overrides body)")
			} else {
				cmd.Flags().StringVar(&releaseType, "release-type", "", "Release type: major|minor (required unless provided by --from-file)")
				cmd.Flags().StringVar(&description, "description", "", "Description for the new version (overrides body)")
			}
		},
	}
}

func versionCreateLong(noun string) string {
	if noun == "workflow" {
		return `Deploy a new workflow version. Workflows version differently from
processors: each deploy is a named snapshot rather than a major/minor bump.
Pass --from-file with the API body (inline JSON, path, file:// URI, or - for
stdin), or use --name to name the deployed version.

Once deployed, refer to the version by its name in extend run --version.`
	}
	return `Publish a new version of the draft.

Pass --release-type major|minor (or provide the full API body via --from-file
which must include releaseType). Use --description to record what changed.

Versions are immutable: future updates create new versions; past versions
remain reachable via the version number. Workflow versioning differs (uses
named deploys instead of major/minor); see ` + "`extend workflows versions create --help`."
}

func versionCreateExamples(noun, plural, exampleID string) []Example {
	if noun == "workflow" {
		return []Example{
			{Label: "Named deploy", Cmd: fmt.Sprintf(`extend %s versions create %s --name "v2-with-review"`, plural, exampleID)},
			{Label: "From file", Cmd: fmt.Sprintf("extend %s versions create %s --from-file deploy.json", plural, exampleID)},
		}
	}
	return []Example{
		{Label: "Minor release", Cmd: fmt.Sprintf(`extend %s versions create %s --release-type minor --description "Added line_items field"`, plural, exampleID)},
		{Label: "From file", Cmd: fmt.Sprintf("extend %s versions create %s --from-file release.json", plural, exampleID)},
	}
}

func versionCreateGotchas(noun string) []string {
	if noun == "workflow" {
		return []string{
			"Workflows use named deploys; do not pass --release-type.",
			"Once deployed, the name is how 'extend run --version' refers to the deploy.",
		}
	}
	return []string{
		"--release-type is required unless provided in the JSON body (must be 'major' or 'minor').",
		"Versions are immutable; future updates create new versions, not edits.",
	}
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

func (a processorAccessor[T, TS, V, VS]) createDoc(app *App) *CommandDoc {
	var fromFile, name string
	return &CommandDoc{
		Use:     "create",
		Summary: fmt.Sprintf("Create %s %s", articleFor(a.noun), a.noun),
		Triggers: []string{
			fmt.Sprintf("create a new %s", a.noun),
			fmt.Sprintf("register a %s in the workspace", a.noun),
			fmt.Sprintf("set up a fresh %s draft", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to create %s %s in the current workspace. The new %s starts as
a draft (no published version); use 'extend %s versions create' once
you're ready to deploy.`, articleFor(a.noun), a.noun, a.noun, a.pluralNoun),
		Details: fmt.Sprintf(`Pass --from-file with the full API body (inline JSON, path, file:// URI, or
- for stdin); --name overrides any name in the body.

For the request body shape, copy from an existing %s:

    extend %s versions get <existing-id> 1.0 > template.json

Then edit and pass via --from-file.`, a.noun, a.pluralNoun),
		Examples: []Example{
			{Label: "From file with name", Cmd: fmt.Sprintf(`extend %s create --from-file %s.json --name "My %s"`, a.pluralNoun, a.noun, a.noun)},
			{Label: "From stdin", Cmd: fmt.Sprintf("cat %s.json | extend %s create --from-file -", a.noun, a.pluralNoun)},
		},
		Gotchas: []string{
			"--from-file is required to provide the API body.",
			"The new resource starts as a draft; publish via 'versions create' to deploy.",
		},
		SeeAlso: []string{
			fmt.Sprintf("%s list", a.pluralNoun),
			fmt.Sprintf("%s update", a.pluralNoun),
			fmt.Sprintf("%s versions create", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON body, path, file:// URI, or '-' for stdin")
			cmd.Flags().StringVar(&name, "name", "", "Name (overrides body)")
		},
	}
}

func (a processorAccessor[T, TS, V, VS]) updateDoc(app *App) *CommandDoc {
	var fromFile, name string
	return &CommandDoc{
		Use:     fmt.Sprintf("update <%s-id>", a.noun),
		Summary: fmt.Sprintf("Update an existing %s", a.noun),
		Triggers: []string{
			fmt.Sprintf("update a %s draft", a.noun),
			fmt.Sprintf("change a %s configuration", a.noun),
			fmt.Sprintf("rename or patch a %s", a.noun),
		},
		WhenToUse: fmt.Sprintf(`Use to update an existing %s's draft config. Updates apply to the
draft version only; they do NOT affect already-deployed versions. Use
'extend %s versions create' to publish the updated draft as a new
version.`, a.noun, a.pluralNoun),
		Details: `Pass --from-file with a full or partial JSON body (inline JSON, path,
file:// URI, or - for stdin); --name overrides any name in the body.`,
		Examples: []Example{
			{Label: "From patch file", Cmd: fmt.Sprintf("extend %s update %s --from-file patch.json", a.pluralNoun, a.exampleID)},
			{Label: "Rename only", Cmd: fmt.Sprintf(`extend %s update %s --name "New name"`, a.pluralNoun, a.exampleID)},
		},
		Gotchas: []string{
			"Updates apply only to the draft; deployed versions are immutable.",
			"To deploy the updated draft, use 'versions create' afterward.",
		},
		SeeAlso: []string{
			fmt.Sprintf("%s get", a.pluralNoun),
			fmt.Sprintf("%s versions create", a.pluralNoun),
		},
		Output: OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:   cobra.ExactArgs(1),
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
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&fromFile, "from-file", "", "JSON patch body, path, file:// URI, or '-' for stdin")
			cmd.Flags().StringVar(&name, "name", "", "New name (overrides body)")
		},
	}
}

// articleFor picks "a" or "an" based on the first letter only.
// Phonetic edge cases (silent h, X/M/etc. with vowel sound) are not
// handled because the only nouns this is called with are: extractor,
// classifier, splitter, workflow, evaluation. Don't trust this for
// arbitrary English.
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

// capitalize uppercases the first byte; safe for the ASCII outputs of
// articleFor ("a" → "A", "an" → "An").
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
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

// readJSONFile accepts inline JSON, a path, stdin (-), or an absolute
// file:// URI, then validates JSON syntax. The error message names the
// flag for clarity ("--config: invalid JSON at offset 42: ..."). We
// use json.Unmarshal rather than json.Valid so a malformed body
// surfaces the offset of the syntax error; json.Valid only returns
// bool, which forces a "somewhere in the body" error message that's
// hard to act on. Returns the raw bytes as json.RawMessage so callers
// can plug it directly into a struct field.
func readJSONFile(path, flag string) (json.RawMessage, error) {
	data, err := readJSONSource(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", flag, err)
	}
	var probe json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", flag, err)
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
		return "", fmt.Errorf("parse file URI %q: %w", source, err)
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

// processorListReqOpts is a tiny helper that translates the CLI-side
// listProcessorsOptions into the SortBy/SortDir/MaxPageSize/NextPageToken
// trio every processor list endpoint accepts. Returning typed pointers
// only when non-empty mirrors the SDK's omit-when-nil idiom.
func processorListReqOpts(opts listProcessorsOptions) (sortBy *extend.SortBy, sortDir *extend.SortDir, maxPageSize *extend.MaxPageSize, nextPageToken *extend.NextPageToken, err error) {
	if opts.SortBy != "" {
		sb, e := extend.NewSortByFromString(opts.SortBy)
		if e != nil {
			err = fmt.Errorf("--sort-by: %w", e)
			return
		}
		sortBy = &sb
	}
	if opts.SortDir != "" {
		sd, e := extend.NewSortDirFromString(opts.SortDir)
		if e != nil {
			err = fmt.Errorf("--sort: %w", e)
			return
		}
		sortDir = &sd
	}
	if opts.Limit > 0 {
		ps := extend.MaxPageSize(opts.Limit)
		maxPageSize = &ps
	}
	if opts.PageToken != "" {
		t := extend.NextPageToken(opts.PageToken)
		nextPageToken = &t
	}
	return
}

func processorVersionListReqOpts(opts listProcessorVersionsOptions) (sortDir *extend.SortDir, maxPageSize *extend.MaxPageSize, nextPageToken *extend.NextPageToken, err error) {
	if opts.SortDir != "" {
		sd, e := extend.NewSortDirFromString(opts.SortDir)
		if e != nil {
			err = fmt.Errorf("--sort: %w", e)
			return
		}
		sortDir = &sd
	}
	if opts.Limit > 0 {
		ps := extend.MaxPageSize(opts.Limit)
		maxPageSize = &ps
	}
	if opts.PageToken != "" {
		t := extend.NextPageToken(opts.PageToken)
		nextPageToken = &t
	}
	return
}
