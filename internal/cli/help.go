package cli

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
)

// Annotation keys used on Cobra commands. Help topics introspect these to
// render up-to-date reference tables, so adding or renaming a value is a
// documented protocol change — verification tests fail builds when a new
// command lacks required annotations or sets one to an unknown value.
const (
	// AnnotOutputTTY: format rendered when stdout is a terminal and no
	// --output flag is set. Value must be a member of OutputModes.
	AnnotOutputTTY = "output.tty"
	// AnnotOutputPipe: format rendered when stdout is not a terminal and no
	// --output flag is set. Value must be a member of OutputModes.
	AnnotOutputPipe = "output.pipe"
	// AnnotWaitProfile: which client.WaitProfile this command's wait loop
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
	string(client.ProfileShort),
	string(client.ProfileLong),
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

// SetIOAnnotations is a small convenience for commands that just specify a
// pair of (TTY, pipe) defaults and don't wait. Use the explicit annotation
// keys when you need wait or lifecycle annotations too.
func SetIOAnnotations(cmd *cobra.Command, tty, pipe OutputMode) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[AnnotOutputTTY] = string(tty)
	cmd.Annotations[AnnotOutputPipe] = string(pipe)
	if _, ok := cmd.Annotations[AnnotWaitProfile]; !ok {
		cmd.Annotations[AnnotWaitProfile] = "n/a"
	}
	if _, ok := cmd.Annotations[AnnotWaitDefault]; !ok {
		cmd.Annotations[AnnotWaitDefault] = "n/a"
	}
}

// SetWaitAnnotations records both wait-related annotations on a command.
// Pair with SetIOAnnotations.
func SetWaitAnnotations(cmd *cobra.Command, profile client.WaitProfile, defaultsToWait bool) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[AnnotWaitProfile] = string(profile)
	if defaultsToWait {
		cmd.Annotations[AnnotWaitDefault] = "true"
	} else {
		cmd.Annotations[AnnotWaitDefault] = "false"
	}
}

// SetLifecycleFailureCodes records the run-status set that causes non-zero
// exit. Pass empty for commands that don't gate exit on run status.
func SetLifecycleFailureCodes(cmd *cobra.Command, statuses ...client.RunStatus) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if len(statuses) == 0 {
		cmd.Annotations[AnnotLifecycleFailureCodes] = ""
		return
	}
	out := ""
	for i, s := range statuses {
		if i > 0 {
			out += ","
		}
		out += string(s)
	}
	cmd.Annotations[AnnotLifecycleFailureCodes] = out
}

// IsRunnableLeaf reports whether cmd is a leaf command we expect to set
// output annotations on. Umbrella commands with subcommands but no Run/RunE
// just print help and don't carry annotations.
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
