package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
)

// This file is the SKILL.md generator: a pure function over the typed
// CommandDoc tree plus two cobra commands (`extend skill` and
// `extend skill install`) that surface it. The body intentionally stays
// under ~5,000 tokens so it fits comfortably in agent skill catalogs
// (Claude Code, Codex, OpenCode, and any other consumer of the
// agentskills.io standard).
//
// Body composition (in order):
//   1. YAML frontmatter (`name`, `description`)
//   2. One-paragraph intro
//   3. Authentication summary + pointer to `extend help auth`
//   4. "Pick the right action" table (generated from Group:"Actions" leaves)
//   5. Wait / async / watch (hand-written)
//   6. Output (one paragraph + pointer to `extend help output`)
//   7. Pagination (verbatim guidance — the highest-leverage gotcha)
//   8. Command catalog (generated, grouped by Group label)
//   9. Topics pointer (one line per `Help topics` entry)
//  10. Common gotchas (curated subset of high-leverage Gotchas)

const skillName = "extend"

// skillDescription is the YAML `description` field. Front-loads the
// trigger phrases so an agent's catalog matcher sees the action verbs up
// front. Keep under ~1,000 chars; clients that elide the body still see
// this string when deciding whether to load the skill.
const skillDescription = `Use when working with the Extend document AI platform: extracting structured data from PDFs and images, parsing documents to text or markdown, classifying or splitting multi-document bundles, filling PDF forms, or running multi-step Extend workflows. The CLI is the canonical agent-facing surface and integrates with the same extractors, classifiers, splitters, and workflows configured in the Extend dashboard.`

// RenderSkill produces the full SKILL.md content (frontmatter + markdown
// body) for the given doc tree. Pure function: no I/O, no API calls, no
// filesystem access. Stable: equivalent input produces equivalent output
// regardless of environment. Tests rely on these properties.
func RenderSkill(root *CommandDoc) string {
	var b strings.Builder

	writeSkillFrontmatter(&b)
	writeSkillIntro(&b)
	writeSkillAuth(&b)
	writeSkillPickActions(&b, root)
	writeSkillWait(&b)
	writeSkillOutput(&b)
	writeSkillPagination(&b)
	writeSkillCatalog(&b, root)
	writeSkillTopics(&b, root)
	writeSkillGotchas(&b, root)

	return b.String()
}

func writeSkillFrontmatter(b *strings.Builder) {
	fmt.Fprintln(b, "---")
	fmt.Fprintf(b, "name: %s\n", skillName)
	fmt.Fprintf(b, "description: %s\n", skillDescription)
	fmt.Fprintln(b, "---")
	fmt.Fprintln(b)
}

func writeSkillIntro(b *strings.Builder) {
	b.WriteString(`# Extend CLI

The ` + "`extend`" + ` CLI is the canonical agent-facing surface for the Extend
document AI platform. It runs extractors, classifiers, splitters, parsers,
PDF form-fill (edit), and multi-step workflows; lists and inspects the runs
and batches those produce; and manages the underlying processor resources.

`)
}

func writeSkillAuth(b *strings.Builder) {
	b.WriteString("## Authentication\n\n")
	b.WriteString("Set `EXTEND_API_KEY` in the environment:\n\n")
	b.WriteString("    export EXTEND_API_KEY=sk_xxx\n\n")
	b.WriteString("Other env vars:\n\n")
	for _, ev := range client.EnvVars {
		if ev.Name == client.EnvAPIKey {
			continue
		}
		fmt.Fprintf(b, "- `%s` — %s\n", ev.Name, ev.Description)
	}
	b.WriteString("\nDefault region is the Extend production endpoint. Region selection: `--region us|us2|eu` flag, or `EXTEND_REGION` env var. Run `extend help auth` for the full reference.\n\n")
}

// writeSkillPickActions renders the "which verb do I run" table dynamically
// from the Group:"Actions" leaves at the top level. Each row is the
// command's first-form invocation plus its Summary; the agent decides
// based on context which to pick. The `<input>` paragraph is hand-written
// because it applies uniformly across action verbs.
func writeSkillPickActions(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Pick the right action\n\n")
	b.WriteString("| Need | Command |\n")
	b.WriteString("|---|---|\n")

	for _, sub := range root.Subcommands {
		if sub.Group != "Actions" {
			continue
		}
		// Top-level action verb only; subcommands (e.g. `extract batch`)
		// are listed in the catalog below.
		fmt.Fprintf(b, "| %s | `extend %s` |\n", sub.Summary, sub.Use)
	}

	b.WriteString("\n`<input>` is a local file path (auto-uploaded), a `file_xxx` ID, or an `https://` URL.\n\n")
	b.WriteString("For batch operations (up to 1,000 inputs in one run), use `<verb> batch`.\n\n")
}

func writeSkillWait(b *strings.Builder) {
	b.WriteString(`## Wait, async, watch

Action verbs (` + "`extract`/`classify`/`parse`/`split`/`edit`" + `) **wait by
default** for terminal state and print the result. Pass ` + "`--wait=false`" + ` to
return the run ID immediately and exit.

` + "`extend run`" + ` (workflow runs) is **async by default** because workflow runs
can take minutes to hours. Pass ` + "`--wait`" + ` to block on it.

Follow a run by ID, regardless of type:

    extend runs watch <run-id>

The run type is auto-detected from the ID prefix (` + "`exr_`, `pr_`, `clr_`," + `
` + "`splr_`, `workflow_run_`, `edr_`" + `). Use ` + "`--exit-status`" + ` to gate downstream
scripts on success:

    extend runs watch exr_xxx --exit-status && downstream-script.sh

To check current state without polling: ` + "`extend runs get <id>`" + `.

For the per-command wait/profile/failure-status table, run ` + "`extend help lifecycle`" + `.

`)
}

func writeSkillOutput(b *strings.Builder) {
	b.WriteString(`## Output

Default output when stdout is piped is JSON. TTY default varies per command.
Override with ` + "`-o json|yaml|raw|id|table|markdown`" + `; filter structured output
with ` + "`--jq <expr>`" + `.

For just the ID: ` + "`-o id`" + `. For just one field: ` + "`--jq '.path' -o raw`" + `. For
the full per-command default table: ` + "`extend help output`" + `.

`)
}

func writeSkillPagination(b *strings.Builder) {
	b.WriteString(`## Pagination (the most common agent mistake)

List commands return one page at a time. Iterate with ` + "`--page-token`" + `, NOT
` + "`--all`" + `:

    FILTERS=(--type extract --using ex_xxx --status PROCESSED)
    tok=""
    while :; do
        page=$(extend runs list "${FILTERS[@]}" --page-token "$tok" -o json)
        echo "$page" | jq '.data[]'
        tok=$(echo "$page" | jq -r '.nextPageToken')
        [ -z "$tok" ] || [ "$tok" = "null" ] && break
    done

**Page tokens are bound to the originating query.** Repeat the same filter
flags on every paginated call; changing them mid-iteration produces
incorrect results. ` + "`--all`" + ` auto-paginates into one response and can exceed
agent context budgets on busy workspaces.

`)
}

// writeSkillCatalog walks the doc tree and renders one bullet per
// command leaf, grouped by Group label. Group ordering mirrors the
// declaration order in RootDoc.Subcommands so what an agent sees here
// matches what `extend --help` shows.
//
// Each line includes the leaf's args (taken from its Use string) so the
// agent sees `extend extract <input>` rather than just `extend extract`.
func writeSkillCatalog(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Command catalog\n\n")

	groups := []string{"Actions", "Inspection", "Resources"}
	headings := map[string]string{
		"Actions":    "Action verbs",
		"Inspection": "Inspection",
		"Resources":  "Resources",
	}

	type entry struct {
		invocation string // "extract <input>" or "extract batch <input>..."
		summary    string
	}
	bucket := map[string][]entry{}
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		// Find the top-level ancestor Group via the dotted path's first
		// segment, looking it up in root.Subcommands.
		segs := strings.SplitN(strings.TrimPrefix(e.Path, root.Name()+"."), ".", 2)
		if len(segs) == 0 {
			continue
		}
		topName := segs[0]
		var topGroup string
		for _, sub := range root.Subcommands {
			if sub.Name() == topName {
				topGroup = sub.Group
				break
			}
		}
		if topGroup == "" || topGroup == "Help topics" || topGroup == "Agent surface" {
			continue
		}
		// Compose the invocation: parent verbs from the path + leaf's
		// own Use (which already includes args).
		pathVerbs := strings.ReplaceAll(strings.TrimPrefix(e.Path, root.Name()+"."), ".", " ")
		// Strip the leaf's verb from the start; replace with leaf.Use.
		leafName := e.Doc.Name()
		parentVerbs := strings.TrimSuffix(strings.TrimSuffix(pathVerbs, leafName), " ")
		invocation := e.Doc.Use
		if parentVerbs != "" {
			invocation = parentVerbs + " " + e.Doc.Use
		}
		bucket[topGroup] = append(bucket[topGroup], entry{invocation: invocation, summary: e.Doc.Summary})
	}

	for _, g := range groups {
		entries := bucket[g]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(b, "### %s\n\n", headings[g])
		for _, e := range entries {
			fmt.Fprintf(b, "- `extend %s` — %s\n", e.invocation, e.summary)
		}
		b.WriteString("\n")
	}
}

func writeSkillTopics(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Reference topics\n\n")
	b.WriteString("Run `extend help <topic>` for the full reference:\n\n")
	for _, sub := range root.Subcommands {
		if !sub.IsTopic() {
			continue
		}
		fmt.Fprintf(b, "- `extend help %s` — %s\n", sub.Name(), sub.Summary)
	}
	b.WriteString("\nFor per-command help: `extend <command> --help`.\n\n")
}

// writeSkillGotchas surfaces the highest-leverage corrections an agent
// should hear once. It collects every leaf's Gotchas and dedupes; we
// don't trim further because each entry is short and the section is the
// payoff for the whole skill.
func writeSkillGotchas(b *strings.Builder, root *CommandDoc) {
	b.WriteString("## Common gotchas\n\n")
	seen := map[string]bool{}
	var entries []string
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		for _, g := range e.Doc.Gotchas {
			g = strings.TrimSpace(g)
			if g == "" || seen[g] {
				continue
			}
			seen[g] = true
			entries = append(entries, g)
		}
	}
	sort.Strings(entries)
	for _, g := range entries {
		fmt.Fprintf(b, "- %s\n", g)
	}
	b.WriteString("\n")
}

// newSkillDoc registers `extend skill` as a top-level command in the
// "Agent surface" group. Two leaves: this command (prints to stdout) and
// `extend skill install` (writes to disk).
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
frontmatter (name + description), an introduction, authentication
summary, action-verb selection table, wait/async semantics, output and
pagination guidance, the full command catalog grouped by section,
references to ` + "`extend help <topic>`" + `, and a deduplicated list of high-leverage
gotchas. Pure function: no API calls, no filesystem access, no network.

The body targets ~5,000 tokens so it fits comfortably in agent skill
catalogs.`,
		Examples: []Example{
			{Label: "Print to stdout", Cmd: "extend skill"},
			{Label: "Pipe to default location", Cmd: "mkdir -p ~/.agents/skills/extend && extend skill > ~/.agents/skills/extend/SKILL.md"},
			{Label: "Or use install", Cmd: "extend skill install"},
		},
		Gotchas: []string{
			"Output is always to stdout; status messages (none, by design) would go to stderr.",
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
// $HOME/.agents/skills/extend/SKILL.md. Documented in the agentskills.io
// standard as the location every conformant client falls back to.
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
` + "`--target`" + ` to write elsewhere (e.g. for a project-local skill checked into
a repo, or a Claude/Codex/OpenCode-specific path).`,
		Details: `By default, writes to:

    $HOME/.agents/skills/extend/SKILL.md

This is the cross-client convention used by Claude Code, Codex, OpenCode,
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
			"Parent directories are created automatically; permissions are 0o755 for dirs and 0o644 for the file.",
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
