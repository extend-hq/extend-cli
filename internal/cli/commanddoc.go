package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
)

// CommandDoc is the typed source of truth for one node in the CLI's
// documentation tree. Every node — runnable command leaves, organizational
// groups, and dynamically-rendered help topics — is a CommandDoc.
//
// CommandDoc projects into *cobra.Command via Build(), but consumers
// (validation tests, --help renderer, the future SKILL.md generator, future
// MCP tool manifests) read from the CommandDoc tree directly via Walk.
// Cobra is one of N derived views, not the source of truth.
//
// A CommandDoc is exactly one of three kinds, distinguished by which
// fields are set:
//
//   - command leaf: RunE != nil, RenderBody == nil
//   - help topic:   RenderBody != nil
//   - group:        RunE == nil, RenderBody == nil, len(Subcommands) > 0
//
// Validate() rejects ambiguous or empty docs at Build() time.
type CommandDoc struct {
	// Use is the cobra usage string ("extract <input>"). The first
	// whitespace-delimited token is the command name.
	Use string
	// Summary is the one-line description (10–80 chars, capitalised, no
	// trailing period). Becomes cobra's Short and the first line of every
	// SKILL.md catalog entry for this command.
	Summary string
	// Aliases are alternate command names. Optional.
	Aliases []string
	// Group is the human-readable group label ("Actions", "Inspection",
	// "Resources", "Help topics"). Optional for command leaves; required
	// for help topics ("Help topics").
	Group string

	// Triggers are 3+ phrases an agent might match against when deciding
	// whether to invoke this command. Each entry must be lowercase and
	// at least 10 characters. Required for command leaves and help topics;
	// optional for groups.
	Triggers []string

	// WhenToUse is selection guidance: "use this when X; prefer Y for Z".
	// Concise prose addressed to the user. Either WhenToUse or Details
	// must be non-empty for command leaves and groups.
	WhenToUse string
	// Details is the reference body for the command. Free-form prose.
	// Either WhenToUse or Details must be non-empty for command leaves
	// and groups.
	Details string
	// Examples are labeled invocations. Required for command leaves
	// (1 minimum); each entry's Cmd must start with "extend ".
	Examples []Example
	// Gotchas are non-obvious behaviours or common mistakes. Optional;
	// each entry should be a complete sentence ending in a period.
	Gotchas []string
	// SeeAlso lists related command paths (e.g. "runs watch", "extract").
	// Each entry must resolve to a real command in the tree. Optional.
	SeeAlso []string

	// Output declares the default rendering modes for tty and pipe.
	// Required for command leaves; ignored for groups and topics.
	Output OutputSpec
	// Wait is non-nil if this command's run loop polls for terminal state.
	// When set, Failures must be non-empty.
	Wait *WaitSpec
	// Failures lists the run statuses that gate non-zero exit. Must be
	// empty when Wait is nil.
	Failures []client.RunStatus

	// Args is the cobra positional-args validator. When nil, Build picks
	// a sane default (NoArgs for groups, ArbitraryArgs for leaves).
	Args cobra.PositionalArgs
	// RunE is the command body. Required for command leaves; must be nil
	// for groups and topics (Build wires topic RunE from RenderBody).
	RunE func(*cobra.Command, []string) error
	// Configure runs after annotation application; bind flags, set
	// PreRunE hooks, etc. here. Optional.
	Configure func(*cobra.Command)

	// RenderBody, when non-nil, marks this doc as a help topic. The body
	// is computed from the root doc tree at help-time; Build wires both
	// `extend <topic>` and `extend help <topic>` to print it.
	RenderBody func(root *CommandDoc) string

	// Subcommands are children in the doc tree. Build adds each via
	// AddCommand on the parent's projected cobra.Command.
	Subcommands []*CommandDoc
}

// Example is one labeled invocation surfaced in help output and skill
// documentation.
type Example struct {
	// Label is the section header for this example ("Basic", "URL input",
	// "With jq filter"). Required.
	Label string
	// Cmd is the literal command line (e.g. "extend extract foo.pdf").
	// Required; must contain "extend " somewhere so pipeline forms like
	// `ls *.pdf | extend extract batch ...` validate.
	Cmd string
	// Note is an optional one-liner clarifying what the example shows.
	Note string
}

// OutputSpec declares which OutputMode a command renders by default for
// each stream type.
type OutputSpec struct {
	// TTY is the default when stdout is a terminal.
	TTY OutputMode
	// Pipe is the default when stdout is not a terminal.
	Pipe OutputMode
}

// WaitSpec declares a command's polling behaviour for long-running
// operations.
type WaitSpec struct {
	// Profile is the polling cadence (ProfileShort or ProfileLong).
	Profile client.WaitProfile
	// DefaultsToWait records whether the command blocks by default
	// (true) or returns the run ID immediately (false).
	DefaultsToWait bool
}

// Name returns the verb extracted from Use ("extract <input>" → "extract").
func (d *CommandDoc) Name() string {
	if d == nil {
		return ""
	}
	for i, r := range d.Use {
		if r == ' ' || r == '\t' {
			return d.Use[:i]
		}
	}
	return d.Use
}

// IsCommand reports whether this doc represents a runnable command leaf.
func (d *CommandDoc) IsCommand() bool {
	return d != nil && d.RunE != nil && d.RenderBody == nil
}

// IsTopic reports whether this doc represents a dynamically-rendered help
// topic (its body is computed from the root doc tree at help-time).
func (d *CommandDoc) IsTopic() bool {
	return d != nil && d.RenderBody != nil
}

// IsGroup reports whether this doc is a pure umbrella with subcommands and
// no behaviour of its own.
func (d *CommandDoc) IsGroup() bool {
	return d != nil && d.RunE == nil && d.RenderBody == nil && len(d.Subcommands) > 0
}

// IsLeaf reports whether this doc has behaviour and no subcommands.
func (d *CommandDoc) IsLeaf() bool {
	return d != nil && (d.IsCommand() || d.IsTopic()) && len(d.Subcommands) == 0
}

// Entry pairs a CommandDoc with its dotted command path.
type Entry struct {
	// Doc is the node.
	Doc *CommandDoc
	// Path is the dotted command path ("extend.runs.watch") for use in
	// test names, error messages, and skill catalog entries.
	Path string
}

// Walk returns the doc tree in DFS declaration order.
func Walk(root *CommandDoc) []Entry {
	if root == nil {
		return nil
	}
	var out []Entry
	var visit func(d *CommandDoc, prefix string)
	visit = func(d *CommandDoc, prefix string) {
		path := d.Name()
		if prefix != "" {
			path = prefix + "." + path
		}
		out = append(out, Entry{Doc: d, Path: path})
		for _, sub := range d.Subcommands {
			visit(sub, path)
		}
	}
	visit(root, "")
	return out
}

// Validate walks the tree and reports every contract violation it finds.
// Build calls Validate and panics on any error; tests call Validate
// directly so they can report all errors per build instead of just the
// first one. Empty input returns nil.
//
// During the migration, the root doc may legitimately have no Subcommands
// (Phase 1 only ships the foundation). Validate exempts the root from the
// classification check while still enforcing identity rules (Use, Summary).
func Validate(root *CommandDoc) []error {
	if root == nil {
		return nil
	}
	var errs []error
	for _, e := range Walk(root) {
		errs = append(errs, validateNode(e)...)
	}
	errs = append(errs, validateSeeAlso(root)...)
	errs = append(errs, validateTriggerUniqueness(root)...)
	return errs
}

func validateNode(e Entry) []error {
	d := e.Doc
	var errs []error

	// Identity rules apply to every node, including root.
	if strings.TrimSpace(d.Use) == "" {
		errs = append(errs, fmt.Errorf("%s: Use is empty", e.Path))
	}
	// Loose 140-char cap to catch runaway summaries (paragraphs masquerading
	// as one-liners) without forcing accurate multi-flag summaries to drop
	// information the catalog actually needs.
	if got := len(d.Summary); got < 10 || got > 140 {
		errs = append(errs, fmt.Errorf("%s: Summary length %d, want 10..140 (%q)", e.Path, got, d.Summary))
	}
	if d.Summary != "" {
		first := rune(d.Summary[0])
		if first < 'A' || first > 'Z' {
			errs = append(errs, fmt.Errorf("%s: Summary must start with capital (%q)", e.Path, d.Summary))
		}
	}
	if strings.HasSuffix(d.Summary, ".") {
		errs = append(errs, fmt.Errorf("%s: Summary must not end with period (%q)", e.Path, d.Summary))
	}

	isRoot := !strings.Contains(e.Path, ".")

	// Phase 1 transitional: the root may have no Subcommands while command
	// migration is in progress. Skip the classifier and per-kind rules in
	// that case; identity rules above still apply.
	if isRoot && len(d.Subcommands) == 0 {
		return errs
	}

	// Classification: every non-root node must be exactly one kind.
	kinds := 0
	if d.IsCommand() {
		kinds++
	}
	if d.IsGroup() {
		kinds++
	}
	if d.IsTopic() {
		kinds++
	}
	if !isRoot {
		switch {
		case kinds == 0:
			errs = append(errs, fmt.Errorf("%s: not a valid command, group, or topic (set RunE, Subcommands, or RenderBody)", e.Path))
			return errs
		case kinds > 1:
			errs = append(errs, fmt.Errorf("%s: ambiguous classification (multiple of RunE / Subcommands / RenderBody set)", e.Path))
			return errs
		}
	}

	if d.IsCommand() {
		errs = append(errs, validateCommandLeaf(e)...)
	}
	if d.IsTopic() {
		errs = append(errs, validateTopic(e)...)
	}
	if d.IsGroup() && !isRoot {
		errs = append(errs, validateGroup(e)...)
	}
	return errs
}

func validateCommandLeaf(e Entry) []error {
	d := e.Doc
	var errs []error

	if len(d.Triggers) < 3 {
		errs = append(errs, fmt.Errorf("%s: command needs >=3 Triggers (got %d)", e.Path, len(d.Triggers)))
	}
	for i, tr := range d.Triggers {
		if len(tr) < 10 {
			errs = append(errs, fmt.Errorf("%s: Triggers[%d] too short (%q)", e.Path, i, tr))
		}
		if tr != strings.ToLower(tr) {
			errs = append(errs, fmt.Errorf("%s: Triggers[%d] must be lowercase (%q)", e.Path, i, tr))
		}
	}
	if len(d.Examples) < 1 {
		errs = append(errs, fmt.Errorf("%s: command needs >=1 Example", e.Path))
	}
	for i, ex := range d.Examples {
		if strings.TrimSpace(ex.Label) == "" {
			errs = append(errs, fmt.Errorf("%s: Examples[%d].Label is empty", e.Path, i))
		}
		if !strings.Contains(ex.Cmd, "extend ") {
			errs = append(errs, fmt.Errorf("%s: Examples[%d].Cmd must invoke \"extend \" (got %q)", e.Path, i, ex.Cmd))
		}
	}
	if d.Output.TTY == "" || d.Output.Pipe == "" {
		errs = append(errs, fmt.Errorf("%s: command needs Output.TTY and Output.Pipe", e.Path))
	}
	if d.Output.TTY != "" && !validOutputMode(string(d.Output.TTY)) {
		errs = append(errs, fmt.Errorf("%s: Output.TTY %q not a valid OutputMode", e.Path, d.Output.TTY))
	}
	if d.Output.Pipe != "" && !validOutputMode(string(d.Output.Pipe)) {
		errs = append(errs, fmt.Errorf("%s: Output.Pipe %q not a valid OutputMode", e.Path, d.Output.Pipe))
	}
	if d.Wait != nil {
		if !validWaitProfile(string(d.Wait.Profile)) {
			errs = append(errs, fmt.Errorf("%s: Wait.Profile %q not valid", e.Path, d.Wait.Profile))
		}
		if len(d.Failures) == 0 {
			errs = append(errs, fmt.Errorf("%s: Wait set but Failures empty", e.Path))
		}
	} else if len(d.Failures) > 0 {
		errs = append(errs, fmt.Errorf("%s: Failures set but Wait nil", e.Path))
	}
	if strings.TrimSpace(d.WhenToUse) == "" && strings.TrimSpace(d.Details) == "" {
		errs = append(errs, fmt.Errorf("%s: command needs WhenToUse or Details", e.Path))
	}
	return errs
}

func validateTopic(e Entry) []error {
	d := e.Doc
	var errs []error
	if d.Group != "Help topics" {
		errs = append(errs, fmt.Errorf("%s: topic Group must be %q (got %q)", e.Path, "Help topics", d.Group))
	}
	if len(d.Triggers) < 3 {
		errs = append(errs, fmt.Errorf("%s: topic needs >=3 Triggers (got %d)", e.Path, len(d.Triggers)))
	}
	for i, tr := range d.Triggers {
		if len(tr) < 10 {
			errs = append(errs, fmt.Errorf("%s: Triggers[%d] too short (%q)", e.Path, i, tr))
		}
		if tr != strings.ToLower(tr) {
			errs = append(errs, fmt.Errorf("%s: Triggers[%d] must be lowercase (%q)", e.Path, i, tr))
		}
	}
	if d.RunE != nil {
		errs = append(errs, fmt.Errorf("%s: topic must not set RunE (Build wires it from RenderBody)", e.Path))
	}
	if len(d.Subcommands) > 0 {
		errs = append(errs, fmt.Errorf("%s: topic must not have Subcommands", e.Path))
	}
	return errs
}

func validateGroup(e Entry) []error {
	d := e.Doc
	var errs []error
	if strings.TrimSpace(d.WhenToUse) == "" && strings.TrimSpace(d.Details) == "" {
		errs = append(errs, fmt.Errorf("%s: group needs WhenToUse or Details", e.Path))
	}
	return errs
}

// validateSeeAlso checks every SeeAlso entry resolves to a command path.
// Entries are command paths relative to root, written with spaces as
// separators ("runs watch", "extract"). Empty SeeAlso passes silently.
func validateSeeAlso(root *CommandDoc) []error {
	known := map[string]bool{}
	rootName := root.Name()
	for _, e := range Walk(root) {
		// Strip leading "<root>."; the relative path uses spaces.
		rel := strings.TrimPrefix(e.Path, rootName+".")
		if rel == e.Path {
			continue // root itself
		}
		known[strings.ReplaceAll(rel, ".", " ")] = true
	}
	var errs []error
	for _, e := range Walk(root) {
		for _, sa := range e.Doc.SeeAlso {
			if !known[sa] {
				errs = append(errs, fmt.Errorf("%s: SeeAlso %q does not resolve to a command", e.Path, sa))
			}
		}
	}
	return errs
}

// validateTriggerUniqueness asserts no two leaves share an identical
// trigger phrase. Ambiguous triggers degrade catalog quality for agents.
func validateTriggerUniqueness(root *CommandDoc) []error {
	seen := map[string]string{}
	var errs []error
	for _, e := range Walk(root) {
		if !e.Doc.IsLeaf() {
			continue
		}
		for _, tr := range e.Doc.Triggers {
			if owner, ok := seen[tr]; ok {
				errs = append(errs, fmt.Errorf("trigger %q claimed by both %s and %s", tr, owner, e.Path))
				continue
			}
			seen[tr] = e.Path
		}
	}
	return errs
}

// Build projects this CommandDoc and its Subcommands into a *cobra.Command
// tree. It first calls Validate; on any contract violation it panics with
// a single error message listing every problem. The panic is intentional:
// a malformed CommandDoc is a programmer error caught at construction time
// (and surfaced by TestRootDocBuilds in CI).
func (d *CommandDoc) Build() *cobra.Command {
	if d == nil {
		panic("CommandDoc.Build called on nil")
	}
	if errs := Validate(d); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		panic("CommandDoc validation failed:\n  " + strings.Join(msgs, "\n  "))
	}
	return d.build(d)
}

// build is the internal recursive constructor. The root reference is
// threaded through so topic RenderBody callbacks can walk the doc tree.
func (d *CommandDoc) build(root *CommandDoc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     d.Use,
		Aliases: d.Aliases,
		Short:   d.Summary,
		Long:    renderLong(d, root),
		Example: renderExamples(d),
		Args:    d.argsOrDefault(),
	}
	if id := groupIDForLabel(d.Group); id != "" {
		cmd.GroupID = id
	}

	// Register the cobra groups that this node's direct children actually
	// reference. Only fires when this is the build root: groups are a
	// presentation concern of the parent, and a wrapper-built subtree
	// (e.g. `extend files` built standalone for the legacy NewRoot path)
	// must not carry the full canonical group set since its own children
	// don't use those groups.
	if d == root {
		seen := map[string]bool{}
		for _, sub := range d.Subcommands {
			id := groupIDForLabel(sub.Group)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			cmd.AddGroup(&cobra.Group{ID: id, Title: groupTitleForID(id)})
		}
	}

	switch {
	case d.RenderBody != nil:
		// Topic: same body for `extend <topic>` and `extend help <topic>`.
		captured := d.RenderBody
		cmd.RunE = func(cobraCmd *cobra.Command, _ []string) error {
			fmt.Fprint(cobraCmd.OutOrStdout(), captured(root))
			return nil
		}
		cmd.SetHelpFunc(func(cobraCmd *cobra.Command, _ []string) {
			fmt.Fprint(cobraCmd.OutOrStdout(), captured(root))
		})
	case d.RunE != nil:
		cmd.RunE = d.RunE
	}

	applyAnnotations(cmd, d)

	if d.Configure != nil {
		d.Configure(cmd)
	}
	for _, sub := range d.Subcommands {
		cmd.AddCommand(sub.build(root))
	}
	return cmd
}

func (d *CommandDoc) argsOrDefault() cobra.PositionalArgs {
	if d.Args != nil {
		return d.Args
	}
	if d.IsGroup() {
		return cobra.NoArgs
	}
	return nil // leave to the caller's flag-bound expectations
}

// renderLong assembles the cobra Long body from the doc's prose fields.
// For topics, the body is the result of RenderBody(root). For everything
// else, sections are joined with blank lines and emitted in this order:
// WhenToUse, Details, "Notes:" (Gotchas), "See also: ..." (SeeAlso).
// Empty sections are omitted entirely.
func renderLong(d *CommandDoc, root *CommandDoc) string {
	if d.RenderBody != nil {
		return d.RenderBody(root)
	}
	var sections []string
	if s := strings.TrimSpace(d.WhenToUse); s != "" {
		sections = append(sections, s)
	}
	if s := strings.TrimSpace(d.Details); s != "" {
		sections = append(sections, s)
	}
	if len(d.Gotchas) > 0 {
		var gb strings.Builder
		gb.WriteString("Notes:")
		for _, g := range d.Gotchas {
			fmt.Fprintf(&gb, "\n  - %s", strings.TrimSpace(g))
		}
		sections = append(sections, gb.String())
	}
	if len(d.SeeAlso) > 0 {
		sections = append(sections, "See also: "+strings.Join(d.SeeAlso, ", "))
	}
	return strings.Join(sections, "\n\n")
}

// renderExamples joins the Examples list into the cobra Example string.
// Each example becomes an indented "  # Label" header followed by the
// command line; blocks are separated by a blank line. An optional Note
// renders as a trailing "  # Note" line under the command.
func renderExamples(d *CommandDoc) string {
	if len(d.Examples) == 0 {
		return ""
	}
	blocks := make([]string, 0, len(d.Examples))
	for _, ex := range d.Examples {
		var b strings.Builder
		if s := strings.TrimSpace(ex.Label); s != "" {
			fmt.Fprintf(&b, "  # %s\n", s)
		}
		fmt.Fprintf(&b, "  %s", ex.Cmd)
		if s := strings.TrimSpace(ex.Note); s != "" {
			fmt.Fprintf(&b, "\n  # %s", s)
		}
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n\n")
}

// applyAnnotations writes the doc's operational metadata into cobra's
// Annotations map, in the same encoding the existing help-topic renderers
// expect. The annotations are an internal projection detail; readers should
// access typed state via the CommandDoc tree, not by decoding annotations.
func applyAnnotations(cmd *cobra.Command, d *CommandDoc) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if d.IsTopic() {
		cmd.Annotations[HelpTopicAnnotation] = "true"
		return
	}
	if d.IsGroup() {
		return
	}
	if d.Output.TTY != "" {
		cmd.Annotations[AnnotOutputTTY] = string(d.Output.TTY)
	}
	if d.Output.Pipe != "" {
		cmd.Annotations[AnnotOutputPipe] = string(d.Output.Pipe)
	}
	if d.Wait != nil {
		cmd.Annotations[AnnotWaitProfile] = string(d.Wait.Profile)
		if d.Wait.DefaultsToWait {
			cmd.Annotations[AnnotWaitDefault] = "true"
		} else {
			cmd.Annotations[AnnotWaitDefault] = "false"
		}
	} else {
		cmd.Annotations[AnnotWaitProfile] = "n/a"
		cmd.Annotations[AnnotWaitDefault] = "n/a"
	}
	if len(d.Failures) > 0 {
		names := make([]string, len(d.Failures))
		for i, s := range d.Failures {
			names[i] = string(s)
		}
		cmd.Annotations[AnnotLifecycleFailureCodes] = strings.Join(names, ",")
	} else {
		cmd.Annotations[AnnotLifecycleFailureCodes] = ""
	}
	if len(d.Triggers) > 0 {
		cmd.Annotations[AnnotTriggers] = strings.Join(d.Triggers, ",")
	}
}

// groupIDForLabel maps a human-readable Group label ("Actions", "Help
// topics", etc.) to the cobra group ID registered on root. Unknown labels
// map to "" so the cobra command is left ungrouped (sorted under
// "Additional Commands").
func groupIDForLabel(label string) string {
	switch label {
	case "Actions":
		return "actions"
	case "Inspection":
		return "inspection"
	case "Resources":
		return "resources"
	case "Agent surface":
		return "agent"
	case "Help topics":
		return HelpTopicGroupID
	}
	return ""
}

// groupTitleForID returns the cobra-group display title for a given group
// ID.
func groupTitleForID(id string) string {
	switch id {
	case "actions":
		return "Actions:"
	case "inspection":
		return "Inspection:"
	case "resources":
		return "Resources:"
	case "agent":
		return "Agent surface:"
	case HelpTopicGroupID:
		return "Help topics:"
	}
	return id
}
