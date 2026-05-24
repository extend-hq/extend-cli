package cli

import (
	"fmt"
	"sort"
	"strings"

	extend "github.com/extend-hq/extend-go-sdk"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// This file defines the four canonical help topics as *CommandDoc literals
// with RenderBody set. The body functions walk the typed doc tree directly
// (via Walk) and read typed OutputSpec / WaitSpec values rather than
// decoding annotation strings — the architectural payoff of unifying topics
// into the same model as commands.

func newAuthTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "auth",
		Summary: "Authentication: env vars, regions, workspace, API version",
		Group:   "Help topics",
		Triggers: []string{
			"how do i authenticate the extend cli",
			"set up extend api key environment",
			"choose the right region us eu us2",
			"workspace id for org-scoped api keys",
			"explain auth precedence and overrides",
		},
		RenderBody: renderAuthTopicBody,
	}
}

func newOutputTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "output",
		Summary: "Output formats, --jq, color, pagination, per-command defaults",
		Group:   "Help topics",
		Triggers: []string{
			"what output format does each command use",
			"explain extend cli output formats and jq",
			"json vs table vs pretty output",
			"pagination pattern for extend list commands",
			"page tokens versus all flag in extend",
		},
		RenderBody: renderOutputTopicBody,
	}
}

func newLifecycleTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "lifecycle",
		Summary: "Run lifecycle: sync vs async, polling, exit codes, watching",
		Group:   "Help topics",
		Triggers: []string{
			"how do extend runs reach terminal state",
			"explain wait flag and async runs in extend",
			"watch a run for completion and exit status",
			"polling profile and timeout for runs",
			"which statuses cause non-zero exit",
		},
		RenderBody: renderLifecycleTopicBody,
	}
}

func newErrorsTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "errors",
		Summary: "Error envelope, request_id, retry/backoff, common codes",
		Group:   "Help topics",
		Triggers: []string{
			"explain extend api error envelope and request id",
			"retry behavior on rate limit and 5xx",
			"common extend api error codes meaning",
			"interpret invalid_request and not_found",
			"server retryable field semantics",
		},
		RenderBody: renderErrorsTopicBody,
	}
}

// renderAuthTopicBody is a typed-tree consumer that pulls env vars and
// regions from the extendx registries. The doc tree itself isn't
// needed here, but the function signature stays uniform across topics.
func renderAuthTopicBody(_ *CommandDoc) string {
	var b strings.Builder
	b.WriteString("Authentication\n\n")
	b.WriteString("Set EXTEND_API_KEY in your environment to authenticate:\n\n")
	b.WriteString("  export EXTEND_API_KEY=sk_xxx\n\n")
	b.WriteString("Environment variables:\n\n")
	maxNameLen := 0
	for _, ev := range envVars {
		if len(ev.Name) > maxNameLen {
			maxNameLen = len(ev.Name)
		}
	}
	for _, ev := range envVars {
		fmt.Fprintf(&b, "  %-*s  %s\n", maxNameLen, ev.Name, ev.Description)
	}
	b.WriteString("\nRegions:\n\n")
	for _, region := range extendx.KnownRegions() {
		url, _ := extendx.RegionBaseURL(region)
		suffix := ""
		if url == extend.Environments.Production {
			suffix = " (default)"
		}
		fmt.Fprintf(&b, "  %-4s  %s%s\n", region, url, suffix)
	}
	b.WriteString("\nPrecedence:\n\n")
	b.WriteString("  --workspace flag     >  EXTEND_WORKSPACE_ID\n")
	b.WriteString("  --region flag        >  EXTEND_REGION\n")
	b.WriteString("  --env flag           >  EXTEND_ENV\n")
	b.WriteString("  EXTEND_BASE_URL      >  EXTEND_REGION (base URL bypasses region selection)\n")
	b.WriteString("\nMultiple environments:\n\n")
	b.WriteString("  --env <label> reads the API key from EXTEND_<UPPER>_API_KEY instead of\n")
	b.WriteString("  EXTEND_API_KEY, so you can keep separate keys for test/prod side-by-side:\n\n")
	b.WriteString("    export EXTEND_API_KEY=sk_prod_xxx\n")
	b.WriteString("    export EXTEND_TEST_API_KEY=sk_test_xxx\n")
	b.WriteString("    extend --env test runs list --type extract\n\n")
	b.WriteString("  Other env vars (workspace, region) are not split per environment;\n")
	b.WriteString("  the docs note workflow definitions are shared between environments while\n")
	b.WriteString("  runs and data are isolated by the key in use.\n")
	return b.String()
}

// renderOutputTopicBody walks the doc tree to render the per-command
// output-format table. Reads typed OutputSpec values directly — no
// annotation decoding — which is the architectural payoff of unifying
// topics with the typed CommandDoc model.
func renderOutputTopicBody(root *CommandDoc) string {
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

	type row struct{ path, tty, pipe string }
	var rows []row
	pathLen := len("command")
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		if e.Doc.Output.TTY == "" && e.Doc.Output.Pipe == "" {
			continue
		}
		// Convert dotted path "extend.runs.watch" into "extend runs watch".
		spaced := strings.ReplaceAll(e.Path, ".", " ")
		if len(spaced) > pathLen {
			pathLen = len(spaced)
		}
		rows = append(rows, row{path: spaced, tty: string(e.Doc.Output.TTY), pipe: string(e.Doc.Output.Pipe)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })

	fmt.Fprintf(&b, "  %-*s  %-9s  %s\n", pathLen, "command", "on tty", "when piped")
	fmt.Fprintf(&b, "  %s  %s  %s\n", strings.Repeat("-", pathLen), strings.Repeat("-", 9), strings.Repeat("-", 10))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-9s  %s\n", pathLen, r.path, r.tty, r.pipe)
	}

	b.WriteString("\nPagination:\n\n")
	b.WriteString("  List commands return one page at a time. Each response includes a\n")
	b.WriteString("  nextPageToken (visible in JSON output, surfaced as a stderr hint on\n")
	b.WriteString("  TTYs). Pass that value to the next call's --page-token to advance.\n\n")
	b.WriteString("  Page tokens are bound to the originating query on the server, so\n")
	b.WriteString("  every follow-up call must repeat the same filter flags as the\n")
	b.WriteString("  first one (--type, --using, --status, --batch, --sort, etc.).\n")
	b.WriteString("  Changing filters between pages yields incorrect results.\n\n")
	b.WriteString("  Recommended pattern (agents and scripts) — note FILTERS reused:\n\n")
	b.WriteString("      FILTERS=(--type extract --using ex_abc --status PROCESSED)\n")
	b.WriteString("      tok=\"\"\n")
	b.WriteString("      while :; do\n")
	b.WriteString("        page=$(extend runs list \"${FILTERS[@]}\" \\\n")
	b.WriteString("                 --page-token \"$tok\" -o json)\n")
	b.WriteString("        echo \"$page\" | jq '.data[]'    # do work on this page\n")
	b.WriteString("        tok=$(echo \"$page\" | jq -r '.nextPageToken')\n")
	b.WriteString("        [ -z \"$tok\" ] || [ \"$tok\" = \"null\" ] && break\n")
	b.WriteString("      done\n\n")
	b.WriteString("  --all is also supported but discouraged: it auto-paginates every\n")
	b.WriteString("  page into a single response, which can exceed context limits on\n")
	b.WriteString("  busy workspaces. Use it only for interactive shell sessions where\n")
	b.WriteString("  you know the result set is small.\n")
	return b.String()
}

// renderLifecycleTopicBody walks the doc tree for typed Wait / Failures
// values rather than decoding wait.* annotations. Same payoff as the
// output topic.
func renderLifecycleTopicBody(root *CommandDoc) string {
	var b strings.Builder
	b.WriteString("Run lifecycle\n\n")
	b.WriteString("Most action commands (extract, classify, split, parse, edit) wait by\n")
	b.WriteString("default for the run to reach a terminal state, then print the result.\n")
	b.WriteString("Pass --async to return the run ID immediately. Workflow runs (extend\n")
	b.WriteString("run) are different: they return immediately by default; pass --wait to\n")
	b.WriteString("block.\n\n")
	b.WriteString("Polling profiles:\n\n")
	for _, spec := range extendx.WaitProfileSpecs() {
		fmt.Fprintf(&b, "  %-6s  %v -> %v\n", spec.Profile, spec.Interval, spec.MaxInterval)
	}
	b.WriteString("\nPer-command behavior:\n\n")

	type row struct{ path, def, profile, fail string }
	var rows []row
	pathLen := len("command")
	for _, e := range Walk(root) {
		if !e.Doc.IsCommand() {
			continue
		}
		if e.Doc.Wait == nil {
			continue
		}
		spaced := strings.ReplaceAll(e.Path, ".", " ")
		if len(spaced) > pathLen {
			pathLen = len(spaced)
		}
		def := "no"
		if e.Doc.Wait.DefaultsToWait {
			def = "yes"
		}
		fail := "(none)"
		if len(e.Doc.Failures) > 0 {
			names := make([]string, len(e.Doc.Failures))
			for i, s := range e.Doc.Failures {
				names[i] = string(s)
			}
			fail = strings.Join(names, ",")
		}
		rows = append(rows, row{
			path:    spaced,
			def:     def,
			profile: string(e.Doc.Wait.Profile),
			fail:    fail,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })

	fmt.Fprintf(&b, "  %-*s  %-7s  %-7s  %s\n", pathLen, "command", "waits?", "profile", "non-zero on")
	fmt.Fprintf(&b, "  %s  %s  %s  %s\n", strings.Repeat("-", pathLen), strings.Repeat("-", 7), strings.Repeat("-", 7), strings.Repeat("-", 11))
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-7s  %-7s  %s\n", pathLen, r.path, r.def, r.profile, r.fail)
	}

	b.WriteString("\nTerminal states:\n\n")
	for _, s := range extendx.TerminalSuccessStates {
		fmt.Fprintf(&b, "  %-13s  Successful completion.\n", s)
	}
	for _, s := range extendx.TerminalFailureStates {
		switch s {
		case extendx.StatusFailed:
			fmt.Fprintf(&b, "  %-13s  Run failed (server-side error or processing failure).\n", s)
		case extendx.StatusCancelled:
			fmt.Fprintf(&b, "  %-13s  Run was cancelled (parse runs cannot be cancelled).\n", s)
		case extendx.StatusRejected:
			fmt.Fprintf(&b, "  %-13s  Run was rejected (workflow runs only).\n", s)
		}
	}
	for _, s := range extendx.TerminalReviewStates {
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

// renderErrorsTopicBody is mostly hand-written prose; the retry policy
// figures come from the SDK's documented defaults (see the
// extend-go-sdk README). Update both when the SDK ships a new policy.
func renderErrorsTopicBody(_ *CommandDoc) string {
	var b strings.Builder
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
	b.WriteString("  The Extend Go SDK retries failed requests automatically. It treats\n")
	b.WriteString("  408 (Timeout), 429 (Rate limit), and 5xx (Server error) responses as\n")
	b.WriteString("  retryable, applying exponential backoff between attempts. When the\n")
	b.WriteString("  server sends a Retry-After header the SDK honors it verbatim.\n\n")
	b.WriteString("  The default policy is 2 retries per request. The CLI does not expose\n")
	b.WriteString("  a knob to override this today; if you need a different policy on a\n")
	b.WriteString("  busy network, raise it on the Extend Go SDK directly.\n\n")
	b.WriteString("Common error codes\n\n")
	b.WriteString("  401 UNAUTHORIZED         API key missing or invalid.\n")
	b.WriteString("  404 NOT_FOUND            Resource doesn't exist or belongs to a\n")
	b.WriteString("                           different workspace.\n")
	b.WriteString("  422 INVALID_REQUEST      Request body or parameters failed schema\n")
	b.WriteString("                           validation. The message field details\n")
	b.WriteString("                           which field; check it before retrying.\n")
	b.WriteString("  429 RATE_LIMIT_EXCEEDED  Auto-retried with backoff (above).\n")
	b.WriteString("  5xx INTERNAL_ERROR       Auto-retried by the SDK.\n")
	return b.String()
}
