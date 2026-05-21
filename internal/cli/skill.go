package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// SKILL.md generator and installer.
//
// The body intentionally stays slim: just frontmatter, auth, action
// selection, active-behaviour rules, brief wait/pagination, and a
// references section pointing at on-demand detail (references/*.md and
// `extend help <topic>`). Detail content lives in CommandDoc help-topic
// bodies (one per references/*.md file), so the same source is the
// canonical truth for both surfaces.
//
// `extend skill install` writes SKILL.md plus the references/ directory
// to a target directory (default ~/.agents/skills/extend/). There is no
// stdout-dump path; inspect the rendered output by installing to a
// throwaway directory and `cat`ing the files.

const skillName = "extend"

// descriptionVerb pairs a user-intent phrase that drives skill triggering
// with the action-verb command it maps to. The frontmatter description
// is rendered from this table; tests assert each Command resolves in the
// doc tree, so the description cannot drift unnoticed.
type descriptionVerb struct {
	Phrase  string
	Command string
}

var descriptionVerbs = []descriptionVerb{
	{"extracting structured data from PDFs or images", "extract"},
	{"parsing documents to text or markdown", "parse"},
	{"classifying or identifying the type of a document (e.g. telling MSA from SOW from NDA)", "classify"},
	{"splitting multi-document bundles into segments", "split"},
	{"filling PDF forms via a values schema", "edit"},
	{"running multi-step document AI workflows", "run"},
	{"inspecting, watching, or listing Extend runs by ID (exr_, pr_, clr_, splr_, edr_, workflow_run_)", "runs"},
	{"uploading documents to an Extend workspace and managing the resulting file_xxx IDs", "files"},
}

// disambiguationExamples are concrete user-phrasings the description
// surfaces to push the agent into triggering even when the user hasn't
// said "Extend" by name.
//
// IMPORTANT: do not paste eval-set prompts (or paraphrases of them) in
// here. Doing so overfits the description to the test set.
var disambiguationExamples = []string{
	`pull line items from these invoices`,
	`OCR these receipts`,
	`categorize this contract`,
	`fill out this PDF form`,
	`split this combined PDF into individual statements`,
}

// resourceFamily captures one of the four processor families. The skill
// renders these parametrically in the catalog because their command
// shape is identical.
type resourceFamily struct {
	Plural   string
	Singular string
	IDPrefix string
	RunVerb  string
}

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
// state by default. The Wait section asserts these behaviours; a test
// asserts each entry's CommandDoc.Wait.DefaultsToWait matches.
var waitDefaultVerbs = []string{"extract", "classify", "parse", "split", "edit"}

// asyncDefaultVerbs lists the action verbs whose runs are async by
// default (workflow runs only, currently).
var asyncDefaultVerbs = []string{"run"}

// paginationExampleCommand is the command path used in the bash example
// in the Pagination section. A test asserts it resolves and that each
// flag in paginationExampleFlags exists on it.
var paginationExampleCommand = []string{"runs", "list"}

// paginationExampleFlags are the flags referenced in the pagination
// example. Asserted to exist on paginationExampleCommand.
var paginationExampleFlags = []string{"type", "using", "status", "max", "all", "output"}

// renderDescription assembles the YAML `description` field from
// descriptionVerbs and the disambiguation examples.
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

// SkillBundle is everything `extend skill install` writes: the SKILL.md
// body plus one file under references/ for every detail topic.
// Returned by RenderSkillBundle so the install command can compute the
// full output in pure-Go before touching the filesystem.
type SkillBundle struct {
	// SkillMD is the body of SKILL.md (frontmatter + slim markdown).
	SkillMD string
	// References maps relative filename ("workflows.md", "auth.md", …)
	// to the markdown body for references/<name>.
	References map[string]string
}

// RenderSkillBundle returns the SKILL.md content + every reference
// file. Pure function over the typed doc tree: no I/O, no API calls.
func RenderSkillBundle(root *CommandDoc) SkillBundle {
	return SkillBundle{
		SkillMD:    RenderSkill(root),
		References: renderReferences(root),
	}
}

// RenderSkill produces the SKILL.md body (frontmatter + slim markdown).
// Detail lives in references/*.md and `extend help <topic>`; this body
// just gives the agent enough shape to know when to load deeper
// content. Pure function: equivalent input produces equivalent output.
func RenderSkill(root *CommandDoc) string {
	var b strings.Builder
	writeSkillFrontmatter(&b)
	writeSkillAuth(&b)
	writeSkillPickActions(&b, root)
	writeSkillActiveBehaviour(&b)
	writeSkillWait(&b)
	writeSkillPagination(&b)
	writeSkillReferences(&b, root)
	return b.String()
}

// renderReferences returns one file per help topic plus convenience
// pointers. The map keys are filenames relative to a references/
// directory; values are markdown bodies (currently produced by topic
// RenderBody functions).
func renderReferences(root *CommandDoc) map[string]string {
	out := map[string]string{}
	for _, e := range Walk(root) {
		if !e.Doc.IsTopic() {
			continue
		}
		out[e.Doc.Name()+".md"] = e.Doc.RenderBody(root)
	}
	return out
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
	b.WriteString("    export EXTEND_API_KEY=sk_xxx              # required\n")
	b.WriteString("    export EXTEND_REGION=us|us2|eu            # optional, default us\n")
	b.WriteString("    export EXTEND_WORKSPACE_ID=ws_xxx         # required only for org-scoped API keys\n\n")
	b.WriteString("Per-call equivalents: `--region eu`, `--workspace ws_xxx`. For API-version pinning, EXTEND_BASE_URL, multi-environment keys, or auth troubleshooting: `references/auth.md` (or `extend help auth`).\n\n")
}

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
	b.WriteString("Every action verb that needs a processor takes `--using <id>` — the ID prefix tells you the type: `ex_*` (extractors), `cl_*` (classifiers), `spl_*` (splitters), `workflow_*` (workflows). `parse` runs alone (no processor); `edit` takes `--instructions` (free-form prose). Full per-command catalog: `references/commands.md`.\n\n")
}

func writeSkillActiveBehaviour(b *strings.Builder) {
	b.WriteString("## When this skill is active\n\n")
	b.WriteString("- **Documents come from disk, not from messages.** When the user references a document (\"this contract\", \"these invoices\", \"the PDF\") without giving a path, glance at the current working directory for matching files (`*.pdf`, `*.png`, `*.jpg`, `*.tif`) before asking. Real users say \"this PDF\" when there's exactly one in cwd.\n")
	b.WriteString("- **File uploads always go through `extend files upload`.** Never substitute a host-tool File API (e.g. an inline file upload tool that returns its own `file_xxx` ID). The skill's file IDs are only legitimate when produced by `extend files upload` or returned in another `extend` response.\n")
	b.WriteString("- **Run IDs (`exr_`/`pr_`/`clr_`/`splr_`/`edr_`/`workflow_run_`) are Extend's, not the host's.** When the user mentions one, reach for `extend runs get|watch|cancel` — not a host-tool task tracker.\n")
	b.WriteString("- **\"OCR\" alone is ambiguous; the user's intent disambiguates.** If they want specific values out (totals, line items, dates, names) → `extract` with a configured extractor. If they want raw text or markdown of the page → `parse`. \"OCR this receipt and grab the total\" is `extract`, not `parse`.\n")
	b.WriteString("- **Look up the schema before authoring config JSON.** Any flag that takes JSON (`--block-options`, `--advanced-options`, `--config`, `--patch`, `--schema`, `--outputs`) has a nested schema where guessing fails silently — the server accepts unknown fields and ignores them, so the call \"succeeds\" but does nothing. For `extend parse`: read `references/parse-options.md` (or `extend help parse-options`) — e.g. enable barcode decoding with `{\"barcodes\":{\"readingEnabled\":true}}`, NOT `{\"barcodes\":true}`. For other commands: `extend <cmd> --help` plus the canonical OpenAPI at https://docs.extend.ai/openapi.json.\n\n")
}

func writeSkillWait(b *strings.Builder) {
	b.WriteString("## Wait, async, watch\n\n")
	b.WriteString("Action verbs (`extract`/`classify`/`parse`/`split`/`edit`) **wait by default** for terminal state. Pass `--wait=false` to return the run ID immediately. `extend run` (workflow runs) is **async by default** — pass `--wait` to block.\n\n")
	b.WriteString("Follow any run by ID (type auto-detected from the prefix `exr_`/`pr_`/`clr_`/`splr_`/`workflow_run_`/`edr_`):\n\n")
	b.WriteString("    extend runs watch <run-id> --exit-status && downstream-script.sh\n\n")
	b.WriteString("For per-command wait/profile/failure tables, polling cadence, and run-type quirks (parse cancellation, edit-run listability, workflow batches): `references/lifecycle.md` (or `extend help lifecycle`).\n\n")
}

func writeSkillPagination(b *strings.Builder) {
	b.WriteString("## Pagination\n\n")
	b.WriteString("List commands return one page by default. `--max N` auto-paginates internally up to N results; tokens stay hidden. Avoid `--all` in agent contexts (no bound).\n\n")
	b.WriteString(`    extend runs list --type extract --status FAILED --max 100` + "\n\n")
	b.WriteString("For the explicit token-by-token pattern, --jq guidance, and per-command output defaults: `references/output.md` (or `extend help output`).\n\n")
}

// writeSkillReferences emits the "load on demand" section: each topic
// gets a one-line entry naming both the file path and the equivalent
// `extend help <topic>` command, plus a one-sentence trigger hint.
// Order is hand-curated to put the highest-leverage references first.
func writeSkillReferences(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## When you need more detail\n\n")
	b.WriteString("Load these on demand when the situation matches. They live alongside this `SKILL.md` under `references/`, and are also available offline via `extend help <topic>`:\n\n")

	// Hand-curated order, highest-leverage first. Topics not listed
	// here are still discoverable via `extend help` but don't appear in
	// this section. A test asserts every entry corresponds to a real
	// topic.
	order := []string{"commands", "parse-options", "lifecycle", "output", "auth", "errors"}
	topics := map[string]*CommandDoc{}
	for _, e := range Walk(root) {
		if e.Doc.IsTopic() {
			topics[e.Doc.Name()] = e.Doc
		}
	}
	for _, name := range order {
		t, ok := topics[name]
		if !ok {
			continue
		}
		fmt.Fprintf(b, "- **`references/%s.md`** (or `extend help %s`) — %s. %s\n",
			name, name, t.Summary, topicLoadHint(name))
	}
	b.WriteString("\n`extend <command> --help` is always available for any command's flags, examples, and per-command gotchas — reach for it whenever a flag isn't obvious.\n")
	b.WriteString("\nThese commands run offline and never contact the Extend API.\n")
}

// topicLoadHint returns a short directive sentence explaining when an
// agent should reach for the named topic. A test (TestSkillTopicLoadHintsCoverReferenced)
// fails if a topic listed in writeSkillReferences has no hint here.
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
		// "before" rather than "when" — agents tend to author JSON first and
		// look up the schema only after a server error. This phrasing nudges
		// them to read the reference first, which is what the eval S-7 found
		// they were skipping.
	case "parse-options":
		return "Load BEFORE authoring JSON for --chunk-strategy/--block-options/--advanced-options on `extend parse` — the schema is nested and easy to get wrong."
	case "commands":
		return "Use to discover what verbs the CLI exposes; pair with `extend <cmd> --help` for depth."
	}
	return ""
}

// newSkillDoc registers `extend skill` as a pure group under the
// "Agent surface" header. The only verb under it is `install`; there
// is no stdout-dump path (inspect by installing to a throwaway dir).
func newSkillDoc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "skill",
		Summary: "Install the Extend SKILL.md and reference files for agent harnesses",
		Group:   "Agent surface",
		WhenToUse: `Use to install this CLI's agent skill (SKILL.md plus a references/
directory of detail topics) into a harness directory like
~/.agents/skills/extend/. The only verb is "install".`,
		Details: `An agent skill is a directory containing SKILL.md (a short body the
agent loads on activation) plus references/*.md (detail files the agent
loads on demand). See https://agentskills.io/specification for the
format.

` + "`extend skill install`" + ` writes the whole tree in one step. The skill
body and reference content are a pure function of the CLI's typed
command tree, so the installed skill always matches the binary.`,
		Subcommands: []*CommandDoc{newSkillInstallDoc(app)},
	}
}

// newSkillInstallDoc registers `extend skill install`. Writes
// SKILL.md and references/*.md to a target directory. The target is
// resolved permissively (see resolveSkillInstallDir) so any of
// ~/.agents, ~/.agents/skills, or ~/.agents/skills/extend produces the
// same final destination.
func newSkillInstallDoc(app *App) *CommandDoc {
	var target string
	return &CommandDoc{
		Use:     "install",
		Summary: "Install SKILL.md + references/ to ~/.agents/skills/extend (default)",
		Triggers: []string{
			"install the extend cli skill into the agent skills directory",
			"write the skill to ~/.agents/skills/extend",
			"deploy the skill markdown for an agent harness",
		},
		WhenToUse: `Use to install the agent skill (SKILL.md plus references/) for a
harness. Default target is ~/.agents/skills/extend/. Pass --target to
install elsewhere (e.g. a Claude Code-specific dir or a project repo).`,
		Details: `Writes:

  <target>/SKILL.md
  <target>/references/auth.md
  <target>/references/output.md
  <target>/references/lifecycle.md
  <target>/references/errors.md
  <target>/references/parse-options.md
  <target>/references/commands.md

--target is permissive: any of these inputs resolves to the same
location <target>/skills/extend/ when the last segment is "skills",
or .../skills/extend/ when it isn't already.

  --target ~/.agents                  -> ~/.agents/skills/extend/
  --target ~/.agents/skills           -> ~/.agents/skills/extend/
  --target ~/.agents/skills/extend    -> ~/.agents/skills/extend/
  --target ~/.claude                  -> ~/.claude/skills/extend/
  --target ~/.claude/skills/extend    -> ~/.claude/skills/extend/

Existing files at the resolved target are overwritten.`,
		Examples: []Example{
			{Label: "Default location", Cmd: "extend skill install"},
			{Label: "Claude Code", Cmd: "extend skill install --target ~/.claude"},
			{Label: "Explicit final path", Cmd: "extend skill install --target ~/.claude/skills/extend"},
			{Label: "Project-local", Cmd: "extend skill install --target ./.agents/skills/extend"},
		},
		Gotchas: []string{
			"--target is the directory, not a file path. Both SKILL.md and references/ land underneath.",
			"Existing target files are overwritten without prompt; install to a fresh directory first if you want to diff.",
		},
		Output: OutputSpec{TTY: OutputNone, Pipe: OutputNone},
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveSkillInstallDir(target)
			if err != nil {
				return err
			}
			return writeSkillBundle(app, dir, RenderSkillBundle(RootDoc(app)))
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&target, "target", "", "Target directory; resolved permissively so ~/.agents, ~/.agents/skills, and ~/.agents/skills/extend all install to the same place (default: ~/.agents/skills/extend)")
		},
	}
}

// resolveSkillInstallDir normalizes a user-supplied --target into the
// final install directory. The user might pass any of:
//
//   - ""                             (default to ~/.agents/skills/<name>)
//   - "~/.agents"                    (a parent two levels up)
//   - "~/.agents/skills"             (a parent one level up)
//   - "~/.agents/skills/extend"      (the exact final directory)
//
// All four resolve to ".../skills/<name>". This lets us steer agents
// towards the agentskills.io convention without making them remember
// the exact path depth.
func resolveSkillInstallDir(input string) (string, error) {
	if input == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		return filepath.Join(home, ".agents", "skills", skillName), nil
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("resolve target %q: %w", input, err)
	}
	clean := filepath.Clean(abs)
	base := filepath.Base(clean)
	switch base {
	case skillName:
		return clean, nil
	case "skills":
		return filepath.Join(clean, skillName), nil
	default:
		return filepath.Join(clean, "skills", skillName), nil
	}
}

// writeSkillBundle writes SKILL.md plus every references/<name>.md file
// to dir, creating subdirectories as needed. Returns on the first
// error; partial writes are left on disk for the user to inspect.
func writeSkillBundle(app *App, dir string, bundle SkillBundle) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	skillPath := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(bundle.SkillMD), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", skillPath, err)
	}

	refsDir := filepath.Join(dir, "references")
	if len(bundle.References) > 0 {
		if err := os.MkdirAll(refsDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", refsDir, err)
		}
	}
	// Stable order so success output is deterministic in tests.
	names := make([]string, 0, len(bundle.References))
	for name := range bundle.References {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(refsDir, name)
		if err := os.WriteFile(path, []byte(bundle.References[name]), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.ErrOut, "%s Installed %d file%s under %s\n",
		pal.Green("✓"), 1+len(bundle.References), pluralS(len(bundle.References)), dir)
	return nil
}

// pluralS returns "s" if n+1 != 1 (i.e. references count makes total > 1).
func pluralS(refs int) string {
	if 1+refs == 1 {
		return ""
	}
	return "s"
}
