package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// This file is the SKILL.md generator: a pure function over the typed
// CommandDoc tree plus two cobra commands (`extend skill` and
// `extend skill install`) that surface it.
//
// The body intentionally targets ~5,000 tokens / ~500 lines so it fits
// comfortably in agent skill catalogs (Claude Code, Codex, OpenCode, and
// any other consumer of the agentskills.io standard).
//
// Body composition (in order):
//
//  1. YAML frontmatter (`name`, `description` from descriptionVerbs).
//  2. Authentication — minimal: API key + region + pointer to topic.
//  3. Pick the right action — table generated from Group:"Actions" leaves.
//  4. Wait, async, watch — hand-written cross-cutting prose.
//  5. Pagination — verbatim guidance with bash example.
//  6. Common workflows — hand-written multi-command recipes.
//  7. Command reference — per-section emitters (actions, inspection,
//     processor resources [parametric], webhooks, evaluations).
//  8. Reference topics — pointers to `extend help <topic>`.
//
// Hand-written claims about the doc tree (description verbs, resource
// family list, wait-by-default verbs, pagination example commands and
// flags, workflow recipe command paths) are guarded by tests in
// skill_test.go. The renderer + tests + this comment are the contract:
// if a hand-written claim drifts from the typed tree, a test fails.

const skillName = "extend"

// descriptionVerb pairs a user-intent phrase that drives skill triggering
// with the action-verb command it maps to. The frontmatter description is
// rendered from this table; tests assert each Command resolves in the
// doc tree, so the description cannot drift unnoticed.
type descriptionVerb struct {
	// Phrase is the imperative user-intent fragment as it should appear
	// in the description text (lowercase, no leading article).
	Phrase string
	// Command is the corresponding top-level command name. Must resolve
	// in RootDoc.Subcommands.
	Command string
}

// descriptionVerbs is the ordered list of action verbs the skill claims
// to handle. Order is the order they appear in the rendered description.
var descriptionVerbs = []descriptionVerb{
	{"extracting structured data from PDFs or images", "extract"},
	{"parsing documents to text or markdown", "parse"},
	{"classifying documents into configured categories", "classify"},
	{"splitting multi-document bundles into segments", "split"},
	{"filling PDF forms via a values schema", "edit"},
	{"running multi-step document AI workflows", "run"},
}

// disambiguationExamples are concrete user-phrasings the description
// surfaces to push the agent into triggering even when the user hasn't
// said "Extend" by name. Per agentskills.io's "be pushy" guidance: the
// most useful negative-failure case is the user describing the task
// without the brand name.
var disambiguationExamples = []string{
	`pull line items from these invoices`,
	`OCR these receipts`,
	`categorize this contract`,
	`fill out this PDF form`,
	`split this combined PDF into individual statements`,
}

// resourceFamily captures one of the four processor families (extractors,
// classifiers, splitters, workflows). The skill renders these
// parametrically because their command shape is identical; per-family
// expansion in the catalog would be ~115 lines of repetition.
type resourceFamily struct {
	// Plural is the noun used in command paths: "extractors", etc.
	Plural string
	// Singular is the noun used in prose: "extractor", etc.
	Singular string
	// IDPrefix is the ID prefix for instances of this resource: "ex_", etc.
	IDPrefix string
	// RunVerb is the action verb that consumes this resource type:
	// "extract" for extractors, "run" for workflows.
	RunVerb string
}

// resourceFamilies enumerates the four processor families. Order is the
// order they appear in the rendered prose. A test asserts each Plural
// resolves in the doc tree with the expected seven-command shape, so
// adding a fifth family or letting one drift forces an explicit update.
var resourceFamilies = []resourceFamily{
	{Plural: "extractors", Singular: "extractor", IDPrefix: "ex_", RunVerb: "extract"},
	{Plural: "classifiers", Singular: "classifier", IDPrefix: "cl_", RunVerb: "classify"},
	{Plural: "splitters", Singular: "splitter", IDPrefix: "spl_", RunVerb: "split"},
	{Plural: "workflows", Singular: "workflow", IDPrefix: "workflow_", RunVerb: "run"},
}

// processorFamilyCommands is the canonical seven-command shape every
// processor family must expose. Tests verify each family in
// resourceFamilies has exactly these subcommands.
var processorFamilyCommands = []string{
	"list",
	"get",
	"create",
	"update",
	"versions list",
	"versions get",
	"versions create",
}

// waitDefaultVerbs lists the action verbs whose runs wait for terminal
// state by default. The "Wait, async, watch" prose section asserts these
// behaviours; a test asserts each entry's CommandDoc.Wait.DefaultsToWait
// matches the documented value.
var waitDefaultVerbs = []string{"extract", "classify", "parse", "split", "edit"}

// asyncDefaultVerbs lists the action verbs whose runs are async by
// default (workflow runs only, currently). Same guard as
// waitDefaultVerbs.
var asyncDefaultVerbs = []string{"run"}

// paginationExampleCommand is the command path used in the bash example
// in the Pagination section. A test asserts it resolves and that each
// flag in paginationExampleFlags exists on it.
var paginationExampleCommand = []string{"runs", "list"}

// paginationExampleFlags are the flags referenced in the pagination
// example. Asserted to exist on paginationExampleCommand.
var paginationExampleFlags = []string{"type", "using", "status", "page-token", "all", "output"}

// renderDescription assembles the YAML `description` field from
// descriptionVerbs and the disambiguation examples. Public via tests so
// the rendering can be inspected without invoking the full renderer.
func renderDescription() string {
	verbs := make([]string, len(descriptionVerbs))
	for i, v := range descriptionVerbs {
		verbs[i] = v.Phrase
	}
	examples := make([]string, len(disambiguationExamples))
	for i, e := range disambiguationExamples {
		examples[i] = `"` + e + `"`
	}
	return fmt.Sprintf(
		`Use when %s — even if the user describes the task without naming Extend (e.g. %s).`,
		joinSerial(verbs, "or"),
		joinSerial(examples, "or"),
	)
}

// joinSerial joins items with commas and a serial-comma'd final
// conjunction ("a, b, or c"). Empty input returns "".
func joinSerial(items []string, conjunction string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " " + conjunction + " " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", " + conjunction + " " + items[len(items)-1]
}

// RenderSkill produces the full SKILL.md content (frontmatter + markdown
// body) for the given doc tree. Pure function: no I/O, no API calls, no
// filesystem access. Stable: equivalent input produces equivalent output
// regardless of environment. Tests rely on these properties.
func RenderSkill(root *CommandDoc) string {
	var b strings.Builder

	writeSkillFrontmatter(&b)
	writeSkillAuth(&b)
	writeSkillPickActions(&b, root)
	writeSkillWait(&b)
	writeSkillPagination(&b)
	writeSkillWorkflows(&b)
	writeSkillCatalog(&b, root)
	writeSkillTopics(&b, root)

	return b.String()
}

func writeSkillFrontmatter(b *strings.Builder) {
	fmt.Fprintln(b, "---")
	fmt.Fprintf(b, "name: %s\n", skillName)
	fmt.Fprintf(b, "description: %s\n", renderDescription())
	fmt.Fprintln(b, "---")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "# Extend CLI")
	fmt.Fprintln(b)
}

func writeSkillAuth(b *strings.Builder) {
	b.WriteString("## Authentication\n\n")
	b.WriteString("    export EXTEND_API_KEY=sk_xxx\n\n")
	b.WriteString("For region selection, workspace-scoped keys, or API-version pinning, run `extend help auth`.\n\n")
}

// writeSkillPickActions renders the "which verb do I run" table dynamically
// from the Group:"Actions" leaves at the top level. Each row pairs the
// command's Summary with its first-form invocation. Pulled from the live
// tree — adding a new action verb shows up here automatically.
func writeSkillPickActions(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Pick the right action\n\n")
	b.WriteString("| Need | Command |\n")
	b.WriteString("|---|---|\n")

	for _, sub := range root.Subcommands {
		if sub.Group != "Actions" {
			continue
		}
		fmt.Fprintf(b, "| %s | `extend %s` |\n", sub.Summary, sub.Use)
	}

	b.WriteString("\n`<input>` is a local file path (auto-uploaded), a `file_xxx` ID, or an `https://` URL. For batches of up to 1,000 inputs, use `<verb> batch`.\n\n")
}

func writeSkillWait(b *strings.Builder) {
	b.WriteString("## Wait, async, watch\n\n")
	b.WriteString("Action verbs (`extract`/`classify`/`parse`/`split`/`edit`) **wait by default** for terminal state and print the result. Pass `--wait=false` to return the run ID immediately.\n\n")
	b.WriteString("`extend run` (workflow runs) is **async by default** because workflow runs can take minutes to hours. Pass `--wait` to block on it.\n\n")
	b.WriteString("Follow a run by ID, regardless of type:\n\n")
	b.WriteString("    extend runs watch <run-id>\n\n")
	b.WriteString("The run type is auto-detected from the ID prefix (`exr_`, `pr_`, `clr_`, `splr_`, `workflow_run_`, `edr_`). Use `--exit-status` to gate downstream scripts on success:\n\n")
	b.WriteString("    extend runs watch exr_xxx --exit-status && downstream-script.sh\n\n")
	b.WriteString("To inspect current state without polling: `extend runs get <id>`.\n\n")
	b.WriteString("**Run-type quirks** (the things that defy reasonable assumptions):\n\n")
	b.WriteString("- **Edit runs** (`edr_*`) are not listable — the API has no `LIST /edit_runs`. Use `extend runs get edr_xxx` for individual edit runs.\n")
	b.WriteString("- **Parse runs** (`pr_*`) cannot be cancelled; the API rejects the attempt. Other run types support best-effort cancel.\n")
	b.WriteString("- **Workflow batches** (returned by `extend run batch`) have **no GET endpoint**. `extend batches get`/`watch` work only on processor batches (`bpr_*`). Track workflow batches with `extend runs list --type workflow --batch <id>`.\n\n")
	b.WriteString("For the per-command wait/profile/failure-status table: `extend help lifecycle`.\n\n")
}

func writeSkillPagination(b *strings.Builder) {
	b.WriteString("## Pagination (the most common agent mistake)\n\n")
	b.WriteString("List commands return one page at a time. Iterate with `--page-token`, **NOT** `--all`:\n\n")
	b.WriteString(`    FILTERS=(--type extract --using ex_xxx --status PROCESSED)
    tok=""
    while :; do
        page=$(extend runs list "${FILTERS[@]}" --page-token "$tok" -o json)
        echo "$page" | jq '.data[]'
        tok=$(echo "$page" | jq -r '.nextPageToken')
        [ -z "$tok" ] || [ "$tok" = "null" ] && break
    done

`)
	b.WriteString("**Page tokens are bound to the originating query.** Repeat the same filter flags on every paginated call; changing them mid-iteration produces incorrect results. `--all` auto-paginates into one response and can exceed agent context budgets.\n\n")
	b.WriteString("`--jq <expr>` filters JSON output before rendering, but cannot combine with `-o markdown` (markdown is not JSON). Use `-o json --jq '...'` and select the markdown chunk paths instead.\n\n")
}

// writeSkillWorkflows emits hand-authored multi-command recipes for the
// most common end-to-end tasks an agent will perform. Per the
// agentskills.io best-practices guide ("favor procedures over
// declarations"), this teaches the agent how to chain commands rather
// than just listing them.
//
// Every `extend <command>` token in this section is asserted to resolve
// in the doc tree by TestSkillWorkflowsReferenceRealCommands; if a
// command is renamed or removed, the test fails until the recipe is
// updated.
func writeSkillWorkflows(b *strings.Builder) {
	b.WriteString(`## Common workflows

### Stand up an extractor and run it

1. Create the extractor draft from a config body:

       extend extractors create --from-file extractor.json --name "Q3 invoices"

   Returns a new ` + "`ex_xxx`" + ` ID. The draft is editable but not yet deployed.
2. Iterate on the draft as needed:

       extend extractors update ex_xxx --from-file patch.json

3. Publish version 1.0 once the draft is solid:

       extend extractors versions create ex_xxx --release-type major

4. Run extraction against a document:

       extend extract invoice.pdf --using ex_xxx

### Process a folder of inputs and inspect failures

1. Submit all inputs in one batch and capture the batch ID:

       BATCH=$(extend extract batch *.pdf --using ex_xxx --jq '.id' -o raw)

2. Wait for the batch to finish; gate downstream work on success:

       extend batches watch "$BATCH" --exit-status || echo "batch failed"

3. List runs that failed (or any other status) for inspection:

       extend runs list --type extract --batch "$BATCH" --status FAILED -o json

4. Pull a specific failed run's full payload (auto-detects type from prefix):

       extend runs get exr_yyy -o json

### Configure a webhook for workflow completions

1. Create the receiving endpoint and capture the signing secret
   (returned only once — store it):

       extend webhooks endpoints create --url https://x.com/hook \
           --name prod \
           --events workflow_run.completed,workflow_run.failed -o json \
           | jq -r '.signingSecret' > webhook.secret

2. Bind the endpoint to a specific workflow:

       extend webhooks subscriptions create \
           --endpoint whe_xxx --resource workflow_yyy \
           --events workflow_run.completed,workflow_run.failed

3. In your receiver, verify each incoming payload before trusting it:

       extend webhooks verify \
           --signature "$X_EXTEND_REQUEST_SIGNATURE" \
           --timestamp "$X_EXTEND_REQUEST_TIMESTAMP" \
           --secret "$(cat webhook.secret)" \
           --body-file payload.json

### Iterate an extractor against an evaluation set

1. Define an evaluation set scoped to the extractor:

       extend evaluations create \
           --from-file '{"name":"Q3 truth","entityId":"ex_xxx"}'

2. Add ground-truth items in bulk:

       extend evaluations items create evs_yyy --from-file items.json

   Each item is ` + "`{fileId, expectedOutput}`" + `; the response wraps them in
   ` + "`{evaluationSetItems: [...]}`" + `.
3. Iterate on the extractor draft, then publish a new version
   (` + "`extend extractors versions create`" + ` as in workflow 1).
4. Run the evaluation set from the dashboard (CLI does not trigger eval
   runs), then inspect the resulting run's per-item accuracy and metrics:

       extend evaluations runs get esr_zzz -o json

`)
}

// writeSkillCatalog renders the per-command reference, dispatching to
// section-specific emitters. The processor-family section is parametric;
// the others walk subtrees of the doc tree. Each entry is a single line
// (invocation + summary); per-command flags, examples, and gotchas are
// kept in `extend <cmd> --help` to respect the spec's progressive-
// disclosure principle.
func writeSkillCatalog(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Command reference\n\n")
	b.WriteString("One line per command — invocation plus a summary. **Run `extend <command> --help` for flags, examples, and per-command gotchas.** The processor-resource block is parametric (the four families share an identical seven-command shape).\n\n")
	writeCatalogSection(b, root, "Action verbs", "Actions", nil)
	writeCatalogSection(b, root, "Inspection", "Inspection", nil)
	writeCatalogProcessorFamilies(b)
	writeCatalogSubtree(b, root, "Webhooks", "webhooks")
	writeCatalogSubtree(b, root, "Evaluations", "evaluations")
}

// writeCatalogSection emits all command leaves whose top-level ancestor
// has the given Group label. Optionally, skipNames excludes specific
// top-level subtrees (used when a section's content is rendered
// elsewhere, e.g. processor families render parametrically).
func writeCatalogSection(b *strings.Builder, root *CommandDoc, heading, group string, skipNames map[string]bool) {
	type entry struct {
		invocation string
		doc        *CommandDoc
	}
	var entries []entry
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		top := topAncestorName(e, root)
		if skipNames[top] {
			continue
		}
		topGroup := topGroupForName(root, top)
		if topGroup != group {
			continue
		}
		entries = append(entries, entry{invocation: leafInvocation(e, root), doc: e.Doc})
	}
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", heading)
	for _, e := range entries {
		writeCatalogEntry(b, e.invocation, e.doc)
	}
	b.WriteString("\n")
}

// writeCatalogSubtree emits all command leaves under a specific top-level
// command (by name), regardless of Group. Used for sections like
// "Webhooks" and "Evaluations" that warrant their own heading despite
// living under the "Resources" group.
func writeCatalogSubtree(b *strings.Builder, root *CommandDoc, heading, topName string) {
	type entry struct {
		invocation string
		doc        *CommandDoc
	}
	var entries []entry
	prefix := root.Name() + "." + topName
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		if !strings.HasPrefix(e.Path, prefix+".") && e.Path != prefix {
			continue
		}
		entries = append(entries, entry{invocation: leafInvocation(e, root), doc: e.Doc})
	}
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", heading)
	for _, e := range entries {
		writeCatalogEntry(b, e.invocation, e.doc)
	}
	b.WriteString("\n")
}

// writeCatalogProcessorFamilies emits the parametric block for the four
// processor families. Saves ~100 lines of repetition vs. listing all 28
// commands individually. TestSkillResourceFamiliesShareShape asserts each
// family in resourceFamilies actually exposes the seven commands listed
// here; if a family diverges, the test fails until either the prose is
// updated or the family is brought back into shape.
func writeCatalogProcessorFamilies(b *strings.Builder) {
	plurals := make([]string, len(resourceFamilies))
	prefixes := make([]string, len(resourceFamilies))
	for i, f := range resourceFamilies {
		plurals[i] = f.Plural
		prefixes[i] = "`" + f.IDPrefix + "`"
	}

	b.WriteString("### Processor resources\n\n")
	fmt.Fprintf(b, "**%s share an identical seven-command shape.** Substitute `<plural>` and the corresponding ID prefix (%s):\n\n",
		capitalize(joinSerial(plurals, "and")),
		joinSerial(prefixes, "and"),
	)
	b.WriteString("- `extend <plural> list` — Page through processors of this type.\n")
	b.WriteString("- `extend <plural> get <id>` — Show one processor.\n")
	b.WriteString("- `extend <plural> create --from-file body.json` — New draft.\n")
	b.WriteString("- `extend <plural> update <id> --from-file patch.json` — Edit the draft. Deployed versions are immutable; the draft is the only mutable surface.\n")
	b.WriteString("- `extend <plural> versions list <id>` — List published versions.\n")
	b.WriteString("- `extend <plural> versions get <id> <version|draft>` — Show one version (or the draft).\n")
	b.WriteString("- `extend <plural> versions create <id> --release-type major|minor` — Publish the draft as a new version.\n\n")
	b.WriteString("**Workflows differ:** `versions create` uses `--name <deploy-name>` instead of `--release-type`. The deployed name is what `extend run --version` references.\n\n")
}

// writeCatalogEntry renders one command in the catalog as a single line:
// invocation + summary. Per-command examples and gotchas live in
// `extend <cmd> --help` (where they're already projected from
// CommandDoc.Examples and CommandDoc.Gotchas via the cobra command's
// Long); the catalog's job is to expose the *shape* of the surface so
// the agent knows what verbs exist, not to duplicate per-command depth
// in the body.
//
// The dig-deeper section ("When this skill isn't enough") elevates
// `extend <cmd> --help` as the first-class path to per-command flags,
// examples, and gotchas. Tests assert the dig-deeper section names this
// path explicitly.
func writeCatalogEntry(b *strings.Builder, invocation string, d *CommandDoc) {
	fmt.Fprintf(b, "- `extend %s` — %s.\n", invocation, strings.TrimRight(d.Summary, "."))
}

// leafInvocation returns the full invocation string for a leaf:
// "extract <input>" (top-level) or "extract batch <input>..." (nested).
func leafInvocation(e Entry, root *CommandDoc) string {
	pathVerbs := strings.ReplaceAll(strings.TrimPrefix(e.Path, root.Name()+"."), ".", " ")
	leafName := e.Doc.Name()
	parentVerbs := strings.TrimSuffix(strings.TrimSuffix(pathVerbs, leafName), " ")
	if parentVerbs == "" {
		return e.Doc.Use
	}
	return parentVerbs + " " + e.Doc.Use
}

// topAncestorName returns the name of the top-level Subcommand under
// root that contains e, or "" if e is the root itself.
func topAncestorName(e Entry, root *CommandDoc) string {
	rel := strings.TrimPrefix(e.Path, root.Name()+".")
	if rel == e.Path {
		return ""
	}
	parts := strings.SplitN(rel, ".", 2)
	return parts[0]
}

// topGroupForName looks up the Group label of the top-level Subcommand
// with the given name, or "" if not found.
func topGroupForName(root *CommandDoc, name string) string {
	for _, sub := range root.Subcommands {
		if sub.Name() == name {
			return sub.Group
		}
	}
	return ""
}

// writeSkillTopics emits the dig-deeper section. The body of this skill
// shows the CLI's *shape* — verbs, decision rules, high-leverage
// gotchas, and end-to-end recipes. Per the agentskills.io progressive-
// disclosure principle, depth lives behind `extend help <topic>` and
// `extend <command> --help`. This section names each, says explicitly
// when to reach for it, and elevates `extend <command> --help` to the
// same prominence as the four reference topics.
func writeSkillTopics(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## When this skill isn't enough\n\n")
	b.WriteString("The body above shows the CLI's *shape*. For depth, use the help system before guessing:\n\n")
	b.WriteString("- `extend <command> --help` — every flag, multiple worked examples, and the full per-command gotcha list. Reach for this whenever a flag isn't obvious or the catalog example doesn't cover your case.\n")
	for _, sub := range root.Subcommands {
		if !sub.IsTopic() {
			continue
		}
		fmt.Fprintf(b, "- `extend help %s` — %s. %s\n", sub.Name(), sub.Summary, topicLoadHint(sub.Name()))
	}
	b.WriteString("\nThese commands run offline and never contact the Extend API.\n")
}

// topicLoadHint returns a short directive sentence explaining when an
// agent should reach for the named topic. Hand-curated; tested by
// TestSkillTopicLoadHintsCoverAllTopics, which fails if a new topic is
// added without a hint.
func topicLoadHint(name string) string {
	switch name {
	case "auth":
		return "Use on auth errors, when working with org-scoped API keys, or when picking a region."
	case "output":
		return "Use when an output format is unexpected or when writing a non-trivial pagination loop."
	case "lifecycle":
		return "Use when reasoning about run states, polling profiles, or when `--exit-status` should fail."
	case "errors":
		return "Use when interpreting an error envelope, picking up a `request_id`, or filing a support ticket."
	}
	return ""
}

// newSkillDoc registers `extend skill` as a top-level command in the
// "Agent surface" group.
func newSkillDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "skill",
		Summary: "Print a SKILL.md describing this CLI to stdout",
		Group:   "Agent surface",
		Triggers: []string{
			"emit a skill markdown describing the extend cli",
			"generate a skill.md for an agent harness",
			"render an agent-facing skill document",
			"print the agentskills.io skill for extend",
		},
		WhenToUse: `Use when wiring the CLI into an agent harness (Claude Code, Codex,
OpenCode, Cursor, Goose, or anything else that consumes the
agentskills.io standard). Pipe the output to a file or use the install
subcommand to write it to the cross-client default path.`,
		Details: `Walks the typed command tree and emits a SKILL.md with YAML
frontmatter, an authentication primer, an action-selection table,
wait/async semantics, pagination guidance, end-to-end common workflows,
the full command catalog (with processor families rendered
parametrically), and pointers to ` + "`extend help <topic>`" + `. Pure function:
no API calls, no filesystem access, no network.

The body targets ~5,000 tokens to fit comfortably in agent skill catalogs.`,
		Examples: []Example{
			{Label: "Print to stdout", Cmd: "extend skill"},
			{Label: "Pipe to default location", Cmd: "mkdir -p ~/.agents/skills/extend && extend skill > ~/.agents/skills/extend/SKILL.md"},
			{Label: "Or use install", Cmd: "extend skill install"},
		},
		Gotchas: []string{
			"The skill is a pure function of the doc tree; running it does not contact the Extend API.",
		},
		SeeAlso: []string{"skill install"},
		Output:  OutputSpec{TTY: OutputMarkdown, Pipe: OutputMarkdown},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprint(app.IO.Out, RenderSkill(RootDoc(app)))
			return nil
		},
		Subcommands: []*CommandDoc{newSkillInstallDoc(app)},
	}
}

// defaultSkillTarget computes the cross-client default install path:
// $HOME/.agents/skills/extend/SKILL.md.
func defaultSkillTarget() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".agents", "skills", skillName, "SKILL.md"), nil
}

func newSkillInstallDoc(app *App) *CommandDoc {
	var target string
	return &CommandDoc{
		Use:     "install",
		Summary: "Write the SKILL.md to disk (default ~/.agents/skills/extend/SKILL.md)",
		Triggers: []string{
			"install the extend cli skill into the agent skills directory",
			"write the skill to ~/.agents/skills/extend",
			"deploy the skill markdown for an agent harness",
		},
		WhenToUse: `Use to write the SKILL.md to the cross-client agent skills directory
in one step, instead of piping ` + "`extend skill > SKILL.md`" + ` yourself. Pass
` + "`--target`" + ` to write elsewhere (project-local skill checked into a repo,
or a Claude/Codex/OpenCode-specific path).`,
		Details: `By default, writes to ` + "`$HOME/.agents/skills/extend/SKILL.md`" + ` —
the cross-client convention used by Claude Code, Codex, OpenCode,
Cursor, Goose, and other agentskills.io consumers. The directory is
created if missing. The file is overwritten if present.

Override the target with ` + "`--target <path>`" + `. Useful targets:

- ` + "`./SKILL.md`" + ` — alongside the agent harness in a checked-in project
- ` + "`./.agents/skills/extend/SKILL.md`" + ` — project-local cross-client skills dir
- ` + "`~/.claude/skills/extend/SKILL.md`" + ` — Claude Code-specific
- ` + "`~/.codex/skills/extend/SKILL.md`" + ` — Codex-specific`,
		Examples: []Example{
			{Label: "Default location", Cmd: "extend skill install"},
			{Label: "Project-local", Cmd: "extend skill install --target ./.agents/skills/extend/SKILL.md"},
			{Label: "Stdout", Cmd: "extend skill install --target -"},
		},
		Gotchas: []string{
			"Existing target file is overwritten without prompt; pipe to a different path first if you want to compare.",
			"Pass `--target -` to stream to stdout (equivalent to running `extend skill`).",
		},
		SeeAlso: []string{"skill"},
		Output:  OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := RenderSkill(RootDoc(app))

			if target == "-" {
				fmt.Fprint(app.IO.Out, body)
				return nil
			}

			path := target
			if path == "" {
				p, err := defaultSkillTarget()
				if err != nil {
					return err
				}
				path = p
			}

			if dir := filepath.Dir(path); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create directory: %w", err)
				}
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				return fmt.Errorf("write skill: %w", err)
			}

			fmt.Fprintf(app.IO.ErrOut, "%s Wrote %d bytes to %s\n",
				paletteFor(app.IO).Green("✓"), len(body), path)
			return nil
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&target, "target", "", "Output path (default: ~/.agents/skills/extend/SKILL.md; pass '-' for stdout)")
		},
	}
}
