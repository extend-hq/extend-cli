package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// Annotation keys used on Cobra commands. These are now an internal
// projection detail of CommandDoc.Build (see commanddoc.go); reading code
// should walk the typed CommandDoc tree via Walk rather than decoding
// these annotations.
//
// They remain exposed because:
//   - Build emits them as the canonical wire shape, and
//   - The help template's "Learn more" footer reads HelpTopicAnnotation
//     to suppress itself on topic commands.
const (
	// AnnotOutputTTY: format rendered when stdout is a terminal and no
	// --output flag is set. Value must be a member of OutputModes.
	AnnotOutputTTY = "output.tty"
	// AnnotOutputPipe: format rendered when stdout is not a terminal and no
	// --output flag is set. Value must be a member of OutputModes.
	AnnotOutputPipe = "output.pipe"
	// AnnotWaitProfile: which extendx.WaitProfile this command's wait loop
	// uses. Value must be a member of WaitProfileNames.
	AnnotWaitProfile = "wait.profile"
	// AnnotWaitDefault: whether the command blocks by default ("true") or
	// returns immediately and prints an in-flight status ("false"). Value
	// "n/a" is used for commands that never wait.
	AnnotWaitDefault = "wait.default"
	// AnnotLifecycleFailureCodes: comma-separated list of run statuses that
	// cause this command to exit non-zero after the run reaches a terminal
	// state. Empty means the command does not gate exit on run status.
	AnnotLifecycleFailureCodes = "lifecycle.failure_codes"
	// AnnotTriggers: comma-separated list of agent-matching phrases for
	// this command, projected from CommandDoc.Triggers. Consumed by the
	// future SKILL.md generator and any other agent-facing surface.
	AnnotTriggers = "agent.triggers"
)

// OutputMode names a default rendering style. The set is closed; adding a
// new mode means teaching the help-output topic about it.
type OutputMode string

const (
	// OutputJSON: pretty-printed JSON object or array. The script-friendly
	// universal default.
	OutputJSON OutputMode = "json"
	// OutputMarkdown: command-specific markdown rendering (currently `parse`
	// on TTY).
	OutputMarkdown OutputMode = "markdown"
	// OutputTable: tabwriter-aligned human-readable table (lists on TTY).
	OutputTable OutputMode = "table"
	// OutputPretty: command-specific human-friendly summary (e.g. classify
	// prints "✓ <type> (NN% confidence)" on TTY; runs watch prints a
	// spinner). Distinct from OutputJSON because it is not machine-parseable.
	OutputPretty OutputMode = "pretty"
	// OutputID: a single line per result containing only the ID. Useful for
	// commands like `files upload` whose primary output is the new ID.
	OutputID OutputMode = "id"
	// OutputBinary: raw bytes (e.g. `files download`). Not JSON, not text.
	OutputBinary OutputMode = "binary"
	// OutputNone: command writes only status/log lines and an exit code; no
	// stdout payload. (E.g. `webhooks verify`.)
	OutputNone OutputMode = "none"
)

// OutputModes is the closed set of valid annotation values for the
// AnnotOutputTTY and AnnotOutputPipe keys.
var OutputModes = []OutputMode{
	OutputJSON, OutputMarkdown, OutputTable, OutputPretty, OutputID, OutputBinary, OutputNone,
}

func validOutputMode(s string) bool {
	for _, m := range OutputModes {
		if string(m) == s {
			return true
		}
	}
	return false
}

// WaitProfileNames is the set of valid annotation values for AnnotWaitProfile,
// including "n/a" for commands that don't wait.
var WaitProfileNames = []string{
	string(extendx.ProfileShort),
	string(extendx.ProfileLong),
	"n/a",
}

func validWaitProfile(s string) bool {
	for _, p := range WaitProfileNames {
		if p == s {
			return true
		}
	}
	return false
}

// WaitDefaultValues is the set of valid annotation values for AnnotWaitDefault.
var WaitDefaultValues = []string{"true", "false", "n/a"}

func validWaitDefault(s string) bool {
	for _, v := range WaitDefaultValues {
		if v == s {
			return true
		}
	}
	return false
}

// IsRunnableLeaf reports whether cmd is a leaf command we expect to set
// output annotations on. Umbrella commands with subcommands but no Run/RunE
// just print help and don't carry annotations. Retained for the legacy
// help-output renderers; new code should iterate the typed doc tree via
// Walk(RootDoc(app)) and filter on Doc.IsCommand() instead.
func IsRunnableLeaf(cmd *cobra.Command) bool {
	return cmd.Runnable()
}

// AllCommands returns every command in the tree rooted at root in a stable
// order (depth-first, lexicographic). Useful for verification tests and for
// help-topic rendering.
func AllCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		out = append(out, c)
		children := append([]*cobra.Command(nil), c.Commands()...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			visit(child)
		}
	}
	visit(root)
	return out
}

// RunnableLeaves returns every runnable command in the tree, excluding pure
// umbrella commands. Order matches AllCommands.
func RunnableLeaves(root *cobra.Command) []*cobra.Command {
	all := AllCommands(root)
	out := make([]*cobra.Command, 0, len(all))
	for _, c := range all {
		if IsRunnableLeaf(c) {
			out = append(out, c)
		}
	}
	return out
}

// paginationGuidance is the boilerplate paragraph every list command's Long
// references for pagination behavior. Centralising it means the recommended
// pattern (token-by-token, NOT --all, same filters across pages) stays
// consistent across the surface.
const paginationGuidance = `Pagination: pass --max N to fetch up to N total results, auto-paginating
internally. Use --all to fetch every page when you really want everything
(scripts only; agents should bound the fetch). Power users who want
explicit cursor control can use --page-token, but most callers should
not need to see tokens at all. The stderr hint emitted on TTYs prints
the exact next-page command for you when one is needed.`

// HelpTopicAnnotation marks a Cobra command as a runtime-rendered help topic
// rather than a regular CLI verb. Topics are runnable (their Run prints the
// rendered content) but they:
//
//   - opt out of the "Learn more" footer that other commands carry, and
//   - signal to the verification tests that they don't need IO/wait
//     annotations.
//
// CommandDoc.Build sets this annotation on every doc with RenderBody != nil.
const HelpTopicAnnotation = "help_topic"

// HelpTopicGroupID is the Cobra group all help topics share, so they cluster
// together in `extend --help` instead of mixing with completion/version under
// "Additional Commands".
const HelpTopicGroupID = "topics"

// helpTopicNames returns the names of every help-topic command registered on
// root, in registration order.
func helpTopicNames(root *cobra.Command) []string {
	var names []string
	for _, c := range root.Commands() {
		if c.Annotations[HelpTopicAnnotation] == "true" {
			names = append(names, c.Name())
		}
	}
	return names
}

// renderTopicFooter builds the "Learn more" footer that's appended to every
// non-topic command's help output. Pulling the topic list dynamically means
// adding a new topic propagates to every command's --help automatically.
func renderTopicFooter(root *cobra.Command) string {
	names := helpTopicNames(root)
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = "extend help " + n
	}
	return "Learn more:\n  " + strings.Join(parts, "     ")
}

// helpTemplate is the Cobra help template applied to root. It mirrors the
// stock template but appends the "Learn more" footer for non-topic commands.
// The {{topicFooter .}} call dispatches to a template function registered in
// installHelpTemplate.
const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{with topicFooter .}}

{{.}}
{{end}}`

// installHelpTemplate wires the help template and its supporting template
// function on root. The topicFooter template function returns "" for topic
// commands (avoiding recursion) and the footer text for everything else.
func installHelpTemplate(root *cobra.Command) {
	cobra.AddTemplateFunc("topicFooter", func(cmd *cobra.Command) string {
		if cmd == nil {
			return ""
		}
		if cmd.Annotations[HelpTopicAnnotation] == "true" {
			return ""
		}
		return renderTopicFooter(cmd.Root())
	})
	root.SetHelpTemplate(helpTemplate)
}
