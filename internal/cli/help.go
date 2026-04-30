package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
)

// Annotation keys used on Cobra commands. Help topics introspect these to
// render up-to-date reference tables, so adding or renaming a value is a
// documented protocol change; verification tests fail builds when a new
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

// HelpTopicAnnotation marks a Cobra command as a runtime-rendered help topic
// rather than a regular CLI verb. Topics are runnable (their Run prints the
// rendered content) but they:
//
//   - opt out of the "Learn more" footer that other commands carry, and
//   - signal to the verification tests that they don't need IO/wait
//     annotations.
//
// Use registerHelpTopic to construct one; it sets the annotation correctly.
const HelpTopicAnnotation = "help_topic"

// HelpTopicGroupID is the Cobra group all help topics share, so they cluster
// together in `extend --help` instead of mixing with completion/version under
// "Additional Commands".
const HelpTopicGroupID = "topics"

// registerHelpTopic adds a help topic command to root. The render callback is
// invoked at every `extend help <name>` and `extend <name>` invocation, so
// it sees the live command tree and current registry state.
//
// Both invocation paths render the same content: the Run handler covers
// `extend <name>`, and a custom HelpFunc covers `extend help <name>` (which
// otherwise would render Cobra's stock help template against the topic and
// not call Run).
func registerHelpTopic(root *cobra.Command, name, short string, render func(root *cobra.Command) string) *cobra.Command {
	topic := &cobra.Command{
		Use:         name,
		Short:       short,
		Args:        cobra.NoArgs,
		GroupID:     HelpTopicGroupID,
		Annotations: map[string]string{HelpTopicAnnotation: "true"},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(cmd.OutOrStdout(), render(cmd.Root()))
		},
	}
	topic.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Fprint(cmd.OutOrStdout(), render(cmd.Root()))
	})
	root.AddCommand(topic)
	return topic
}

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

// renderAuthTopic builds the body of `extend help auth` from the env-var and
// region registries in internal/client. Adding a new env var to client.EnvVars
// updates this output automatically.
func renderAuthTopic(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("Authentication\n\n")
	b.WriteString("Set EXTEND_API_KEY in your environment to authenticate:\n\n")
	b.WriteString("  export EXTEND_API_KEY=sk_xxx\n\n")
	b.WriteString("Environment variables:\n\n")
	maxNameLen := 0
	for _, ev := range client.EnvVars {
		if len(ev.Name) > maxNameLen {
			maxNameLen = len(ev.Name)
		}
	}
	for _, ev := range client.EnvVars {
		fmt.Fprintf(&b, "  %-*s  %s\n", maxNameLen, ev.Name, ev.Description)
	}
	b.WriteString("\nRegions:\n\n")
	for _, region := range client.KnownRegions() {
		url, _ := client.RegionBaseURL(region)
		suffix := ""
		if url == client.DefaultBaseURL {
			suffix = " (default)"
		}
		fmt.Fprintf(&b, "  %-4s  %s%s\n", region, url, suffix)
	}
	b.WriteString("\nPrecedence:\n\n")
	b.WriteString("  --workspace flag     >  EXTEND_WORKSPACE_ID\n")
	b.WriteString("  --region flag        >  EXTEND_REGION\n")
	b.WriteString("  EXTEND_BASE_URL      >  EXTEND_REGION (base URL bypasses region selection)\n")
	return b.String()
}

// renderOutputTopic builds the body of `extend help output`. The per-command
// default table is generated from the AnnotOutputTTY/AnnotOutputPipe
// annotations on every runnable leaf, so it stays in sync with code.
func renderOutputTopic(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("Output\n\n")
	b.WriteString("The --output (-o) flag selects the rendering format:\n\n")
	b.WriteString("  json       Pretty-printed JSON (script-friendly default for piped output)\n")
	b.WriteString("  yaml       YAML\n")
	b.WriteString("  raw        Unformatted strings (typically used with --jq)\n")
	b.WriteString("  id         Single ID per result (composes with xargs / shell pipes)\n")
	b.WriteString("  table      Tabwriter human-readable table (lists)\n")
	b.WriteString("  markdown   Markdown rendering (parse only)\n\n")
	b.WriteString("Filtering:\n\n")
	b.WriteString("  --jq <expr>    Filter structured output before rendering. Cannot be\n")
	b.WriteString("                 combined with -o markdown.\n\n")
	b.WriteString("Streams: data goes to stdout, status/log lines go to stderr.\n")
	b.WriteString("Color: NO_COLOR=1 disables; CLICOLOR_FORCE=1 forces.\n\n")
	b.WriteString("Per-command default (when --output is not set):\n\n")
	leaves := RunnableLeaves(root)
	pathLen := 0
	rows := make([][3]string, 0, len(leaves))
	for _, c := range leaves {
		if c.Annotations[HelpTopicAnnotation] == "true" {
			continue
		}
		tty := c.Annotations[AnnotOutputTTY]
		pipe := c.Annotations[AnnotOutputPipe]
		if tty == "" && pipe == "" {
			continue
		}
		path := c.CommandPath()
		if len(path) > pathLen {
			pathLen = len(path)
		}
		rows = append(rows, [3]string{path, tty, pipe})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	fmt.Fprintf(&b, "  %-*s  %-9s  %s\n", pathLen, "command", "on tty", "when piped")
	fmt.Fprintf(&b, "  %s  %s  %s\n", strings.Repeat("-", pathLen), strings.Repeat("-", 9), strings.Repeat("-", 10))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-9s  %s\n", pathLen, r[0], r[1], r[2])
	}
	b.WriteString("\nPagination:\n\n")
	b.WriteString("  List commands take --limit (default 20) and --all. Without --all,\n")
	b.WriteString("  only the first page is returned; nextPageToken appears in the JSON\n")
	b.WriteString("  output for manual paging.\n")
	return b.String()
}

// renderRunsTopic builds the body of `extend help runs` from WaitProfileSpecs,
// the wait/lifecycle annotations on each command, and the terminal-state
// constants in client.
func renderRunsTopic(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("Run lifecycle\n\n")
	b.WriteString("Most action commands (extract, classify, split, parse, edit) wait by\n")
	b.WriteString("default for the run to reach a terminal state, then print the result.\n")
	b.WriteString("Pass --async to return the run ID immediately. Workflow runs (extend\n")
	b.WriteString("run) are different: they return immediately by default; pass --wait to\n")
	b.WriteString("block.\n\n")
	b.WriteString("Polling profiles:\n\n")
	for _, spec := range client.WaitProfileSpecs() {
		fmt.Fprintf(&b, "  %-6s  %v -> %v\n", spec.Profile, spec.Interval, spec.MaxInterval)
	}
	b.WriteString("\nPer-command behavior:\n\n")
	leaves := RunnableLeaves(root)
	pathLen := 0
	type row struct{ path, def, profile, fail string }
	rows := make([]row, 0, len(leaves))
	for _, c := range leaves {
		if c.Annotations[HelpTopicAnnotation] == "true" {
			continue
		}
		profile := c.Annotations[AnnotWaitProfile]
		if profile == "" || profile == "n/a" {
			continue
		}
		def := c.Annotations[AnnotWaitDefault]
		fail := c.Annotations[AnnotLifecycleFailureCodes]
		if fail == "" {
			fail = "(none)"
		}
		path := c.CommandPath()
		if len(path) > pathLen {
			pathLen = len(path)
		}
		rows = append(rows, row{path, def, profile, fail})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })
	fmt.Fprintf(&b, "  %-*s  %-7s  %-7s  %s\n", pathLen, "command", "waits?", "profile", "non-zero on")
	fmt.Fprintf(&b, "  %s  %s  %s  %s\n", strings.Repeat("-", pathLen), strings.Repeat("-", 7), strings.Repeat("-", 7), strings.Repeat("-", 11))
	for _, r := range rows {
		waits := r.def
		if waits == "true" {
			waits = "yes"
		} else if waits == "false" {
			waits = "no"
		}
		fmt.Fprintf(&b, "  %-*s  %-7s  %-7s  %s\n", pathLen, r.path, waits, r.profile, r.fail)
	}
	b.WriteString("\nTerminal states:\n\n")
	for _, s := range client.TerminalSuccessStates {
		fmt.Fprintf(&b, "  %-13s  Successful completion.\n", s)
	}
	for _, s := range client.TerminalFailureStates {
		switch s {
		case client.StatusFailed:
			fmt.Fprintf(&b, "  %-13s  Run failed (server-side error or processing failure).\n", s)
		case client.StatusCancelled:
			fmt.Fprintf(&b, "  %-13s  Run was cancelled (parse runs cannot be cancelled).\n", s)
		case client.StatusRejected:
			fmt.Fprintf(&b, "  %-13s  Run was rejected (workflow runs only).\n", s)
		}
	}
	for _, s := range client.TerminalReviewStates {
		fmt.Fprintf(&b, "  %-13s  Paused for human review at the dashboard URL. Terminal\n", s)
		fmt.Fprintf(&b, "  %-13s  but not failed; does NOT cause non-zero exit.\n", "")
	}
	b.WriteString("\nWatching:\n\n")
	b.WriteString("  Use `extend runs watch <id>` for any run, or `extend batches watch`\n")
	b.WriteString("  for batch runs. Both accept --exit-status, which propagates the\n")
	b.WriteString("  terminal status to the exit code:\n\n")
	b.WriteString("      extend runs watch <id> --exit-status && downstream-script.sh\n\n")
	b.WriteString("  Without --exit-status, watch commands exit 0 on any successful\n")
	b.WriteString("  poll regardless of run status. That is useful when you want the\n")
	b.WriteString("  JSON output and plan to inspect the status field yourself.\n\n")
	b.WriteString("Webhooks alternative:\n\n")
	b.WriteString("  For long-running workflow operations, configure a webhook endpoint\n")
	b.WriteString("  instead of polling. See `extend webhooks endpoints --help`.\n")
	return b.String()
}

// renderErrorsTopic builds the body of `extend help errors` from
// client.DefaultRetryConfig, with hand-written prose around it.
func renderErrorsTopic(_ *cobra.Command) string {
	var b strings.Builder
	rc := client.DefaultRetryConfig
	b.WriteString("Errors\n\n")
	b.WriteString("API errors carry a stable envelope:\n\n")
	b.WriteString("  {\n")
	b.WriteString("    \"code\":      \"INVALID_REQUEST\",\n")
	b.WriteString("    \"message\":   \"<human-readable detail>\",\n")
	b.WriteString("    \"retryable\": false,\n")
	b.WriteString("    \"requestId\": \"req_abc123\"\n")
	b.WriteString("  }\n\n")
	b.WriteString("The CLI prints errors to stderr in red and includes the request_id\n")
	b.WriteString("on its own dimmed line. Cite the request_id when filing support\n")
	b.WriteString("tickets so the team can correlate your call to server-side logs.\n\n")
	b.WriteString("Retries\n\n")
	fmt.Fprintf(&b, "  GET requests retry up to %d times on 429 and 5xx errors with\n", rc.MaxAttempts)
	fmt.Fprintf(&b, "  exponential backoff (%v -> %v).\n\n", rc.InitialBackoff, rc.MaxBackoff)
	b.WriteString("  POST requests only retry on 429. POSTs are not assumed idempotent\n")
	b.WriteString("  and will not be replayed on 5xx errors. The server's `retryable`\n")
	b.WriteString("  field overrides this for non-2xx responses where the server\n")
	b.WriteString("  explicitly opts in to retries.\n\n")
	b.WriteString("Common error codes\n\n")
	b.WriteString("  401 UNAUTHORIZED         API key missing or invalid.\n")
	b.WriteString("  404 NOT_FOUND            Resource doesn't exist or belongs to a\n")
	b.WriteString("                           different workspace.\n")
	b.WriteString("  422 INVALID_REQUEST      Request body or parameters failed schema\n")
	b.WriteString("                           validation. The message field details\n")
	b.WriteString("                           which field; check it before retrying.\n")
	b.WriteString("  429 RATE_LIMIT_EXCEEDED  Auto-retried with backoff (above).\n")
	b.WriteString("  5xx INTERNAL_ERROR       Auto-retried for GETs only.\n")
	return b.String()
}

// installHelpTopics registers all four help topics on root and wires the
// help template. Call after the rest of the command tree is built so the
// runtime renderers see every command.
//
// Topic names are intentionally chosen to not collide with any verb in the
// command tree: in particular, the runs-lifecycle topic is named "lifecycle"
// rather than "runs" because `extend runs` is the umbrella for run-management
// subcommands.
func installHelpTopics(root *cobra.Command) {
	root.AddGroup(&cobra.Group{ID: HelpTopicGroupID, Title: "Help topics:"})
	registerHelpTopic(root, "auth", "Authentication: env vars, regions, workspace, API version", renderAuthTopic)
	registerHelpTopic(root, "output", "Output formats, --jq, color, pagination, per-command defaults", renderOutputTopic)
	registerHelpTopic(root, "lifecycle", "Run lifecycle: sync vs async, polling, exit codes, watching", renderRunsTopic)
	registerHelpTopic(root, "errors", "Error envelope, request_id, retry/backoff, common codes", renderErrorsTopic)
	installHelpTemplate(root)
}
