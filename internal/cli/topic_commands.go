package cli

import (
	"fmt"
	"strings"
)

// newCommandsTopicDoc registers `extend help commands` — the per-command
// catalog (invocation + one-line summary). The same body is written to
// references/commands.md during `extend skill install`.
//
// The processor-resource block is rendered parametrically (the four
// families share an identical seven-command shape); the rest is walked
// from the doc tree so the catalog stays accurate as commands are
// added, renamed, or moved between groups.
func newCommandsTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "commands",
		Summary: "Per-command catalog: every CLI verb with a one-line summary",
		Group:   "Help topics",
		Triggers: []string{
			"list every extend cli command with summaries",
			"discover what verbs the extend cli exposes",
			"catalog of extend cli commands",
			"what commands does extend cli have",
		},
		RenderBody: renderCommandsTopicBody,
	}
}

func renderCommandsTopicBody(root *CommandDoc) string {
	var b strings.Builder
	b.WriteString("Command reference\n\n")
	b.WriteString("One line per command — invocation plus a summary. Run `extend <command> --help` for flags, examples, and per-command gotchas. The processor-resource block is parametric (the four families share an identical seven-command shape).\n\n")
	writeCatalogSection(&b, root, "Action verbs", "Actions", nil)
	writeCatalogSection(&b, root, "Inspection", "Inspection", nil)
	writeCatalogProcessorFamilies(&b)
	writeCatalogSubtree(&b, root, "Webhooks", "webhooks")
	writeCatalogSubtree(&b, root, "Evaluations", "evaluations")
	return b.String()
}

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

func writeCatalogSubtree(b *strings.Builder, root *CommandDoc, heading, topName string) {
	type entry struct {
		invocation string
		doc        *CommandDoc
	}
	prefix := root.Name() + "." + topName
	var entries []entry
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
