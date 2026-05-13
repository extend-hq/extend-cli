package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// fakePage is the minimum shape needed to exercise renderList's pagination
// hint: a NextPageToken field for nextPageTokenOf to find via reflection.
type fakePage struct {
	NextPageToken string
}

// TestEnvOutput_FillsWhenFlagUnset checks that EXTEND_OUTPUT is read
// when --output isn't passed, so users (and agent harnesses) can set
// a default format once instead of repeating -o on every call.
func TestEnvOutput_FillsWhenFlagUnset(t *testing.T) {
	t.Setenv("EXTEND_OUTPUT", "json")
	app := &App{}
	applyEnvDefaults(app)
	if app.Format != "json" {
		t.Errorf("expected app.Format=%q, got %q", "json", app.Format)
	}
}

// TestEnvOutput_FlagWins guards precedence: --output beats EXTEND_OUTPUT.
// The user always retains per-call control even if a global default is
// set in the environment.
func TestEnvOutput_FlagWins(t *testing.T) {
	t.Setenv("EXTEND_OUTPUT", "json")
	app := &App{Format: "table"} // simulates --output=table on the command line
	applyEnvDefaults(app)
	if app.Format != "table" {
		t.Errorf("flag should win over env; expected app.Format=%q, got %q", "table", app.Format)
	}
}

// TestEnvOutput_NoEnvNoFlagLeavesEmpty confirms that with neither flag
// nor env set, app.Format stays empty so per-command defaults take
// over (TTY-format table for lists, custom inline output for action
// verbs, etc.).
func TestEnvOutput_NoEnvNoFlagLeavesEmpty(t *testing.T) {
	t.Setenv("EXTEND_OUTPUT", "")
	app := &App{}
	applyEnvDefaults(app)
	if app.Format != "" {
		t.Errorf("expected empty Format when neither flag nor env set, got %q", app.Format)
	}
}

// TestEnvLabel_FillsFromEnvVar mirrors the EXTEND_OUTPUT precedence
// for EXTEND_ENV: the env var fills app.Env when --env isn't passed.
func TestEnvLabel_FillsFromEnvVar(t *testing.T) {
	t.Setenv("EXTEND_ENV", "test")
	app := &App{}
	applyEnvDefaults(app)
	if app.Env != "test" {
		t.Errorf("expected app.Env=%q, got %q", "test", app.Env)
	}
}

// TestEnvLabel_FlagWins guards precedence: --env beats EXTEND_ENV.
func TestEnvLabel_FlagWins(t *testing.T) {
	t.Setenv("EXTEND_ENV", "test")
	app := &App{Env: "staging"} // simulates --env=staging
	applyEnvDefaults(app)
	if app.Env != "staging" {
		t.Errorf("flag should win over env; got %q", app.Env)
	}
}

// TestAPIKeyEnvVar codifies the env-label → env-var-name mapping that
// powers --env. Empty label keeps the historical EXTEND_API_KEY name.
// Non-alphanumerics in the label are stripped so users can pass labels
// like "test-1" without producing invalid var names.
func TestAPIKeyEnvVar(t *testing.T) {
	cases := []struct {
		label string
		want  string
	}{
		{"", "EXTEND_API_KEY"},
		{"test", "EXTEND_TEST_API_KEY"},
		{"TEST", "EXTEND_TEST_API_KEY"},
		{"staging", "EXTEND_STAGING_API_KEY"},
		{"test-1", "EXTEND_TEST1_API_KEY"},
		{"my_env", "EXTEND_MY_ENV_API_KEY"},
		{"   ", "EXTEND_API_KEY"}, // whitespace-only → default
	}
	for _, tc := range cases {
		got := apiKeyEnvVar(tc.label)
		if got != tc.want {
			t.Errorf("apiKeyEnvVar(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

// TestRenderListDefault_TableEvenWhenPiped guards the format-default
// flip: with no -o flag and no --jq, rendering MUST produce the
// human-readable table even when stdout isn't a TTY (i.e. when the
// process is being driven by an agent harness that captures stdout).
// JSON is only chosen when explicitly requested via -o or implied by
// --jq.
func TestRenderListDefault_TableEvenWhenPiped(t *testing.T) {
	ios, _, out, _ := iostreams.Test()
	// Both streams left as non-TTY (the default), simulating a pipe.
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: ""}}
	rows := [][]string{{"ex_abc", "Q3 invoices", "2026-04-30"}}
	if err := renderList(app, pages, []string{"id", "name", "created"}, rows, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	got := out.String()
	// Table output contains the column names (uppercased by the table
	// renderer) and the row values.
	if !strings.Contains(strings.ToLower(got), "id") || !strings.Contains(got, "ex_abc") {
		t.Errorf("expected table output even when stdout is piped, got: %q", got)
	}
	// JSON output would start with {. Make sure we did NOT fall back to JSON.
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Errorf("expected table output (not JSON) when -o is unset, got: %q", got)
	}
}

// TestRenderListDefault_JQImpliesJSON confirms that passing --jq
// switches the default to JSON (because jq operates on JSON), even
// though the format-default flip otherwise picks the table form.
func TestRenderListDefault_JQImpliesJSON(t *testing.T) {
	ios, _, out, _ := iostreams.Test()
	app := &App{IO: ios, JQ: ".[].id"}

	pages := []any{fakePage{NextPageToken: ""}}
	rows := [][]string{{"ex_abc"}}
	// Pass a tiny one-row payload so jq has something to filter.
	type item struct {
		ID string `json:"id"`
	}
	pages = []any{[]item{{ID: "ex_abc"}}}
	if err := renderList(app, pages, []string{"id"}, rows, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != `"ex_abc"` {
		t.Errorf("expected jq-filtered JSON output, got: %q", got)
	}
}

// TestRenderListDefault_ExplicitJSON confirms that -o json still
// produces JSON output regardless of TTY state. This is the path
// scripts and agents take when they want to parse the result.
func TestRenderListDefault_ExplicitJSON(t *testing.T) {
	ios, _, out, _ := iostreams.Test()
	app := &App{IO: ios, Format: "json"}

	type item struct {
		ID string `json:"id"`
	}
	pages := []any{[]item{{ID: "ex_abc"}}}
	if err := renderList(app, pages, []string{"id"}, [][]string{{"ex_abc"}}, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"id"`) || !strings.Contains(got, `"ex_abc"`) {
		t.Errorf("expected JSON when -o json is set, got: %q", got)
	}
}

// TestRenderListDefault_EmptyMessageWhenPiped guards that an empty
// list renders the human-friendly emptyMsg even when stdout is piped
// (the same path TestWebhooksEndpointsList_Empty exercises through
// the full command tree).
func TestRenderListDefault_EmptyMessageWhenPiped(t *testing.T) {
	ios, _, out, _ := iostreams.Test()
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: ""}}
	if err := renderList(app, pages, []string{"id"}, [][]string{}, "No webhook endpoints."); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if !strings.Contains(out.String(), "No webhook endpoints.") {
		t.Errorf("expected empty-list message, got: %q", out.String())
	}
}

// TestPaginationHint_AppearsOnTTYWithMore checks that the pagination hint
// fires to stderr when the last page has a NextPageToken and stderr is a
// TTY, and that it includes a runnable next-page command preserving the
// caller's filter flags.
func TestPaginationHint_AppearsOnTTYWithMore(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	ios.SetStderrTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	app := &App{IO: ios}

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("page-token", "", "")
	cmd.Flags().Bool("all", false, "")
	_ = cmd.Flags().Set("type", "extract")

	pages := []any{fakePage{NextPageToken: "tok_more"}}
	rows := [][]string{{"ex_1", "one", ""}}
	if err := renderListForCmd(cmd, app, pages, []string{"id", "name", "created"}, rows, "none"); err != nil {
		t.Fatalf("renderListForCmd: %v", err)
	}
	got := errOut.String()
	if !strings.Contains(got, "more results available") {
		t.Errorf("stderr missing pagination hint, got: %q", got)
	}
	if !strings.Contains(got, "tok_more") {
		t.Errorf("stderr should include the next-page-token; got: %q", got)
	}
	if !strings.Contains(got, "--type extract") {
		t.Errorf("stderr should preserve user-set filter flags in the next-page command; got: %q", got)
	}
	if !strings.Contains(got, "--page-token tok_more") {
		t.Errorf("stderr should include --page-token in the next-page command; got: %q", got)
	}
}

// TestPaginationHint_DropsAllAndOriginalToken ensures --all and the
// previous --page-token are not carried into the next-page command. --all
// is incompatible with explicit pagination; the previous page-token would
// fetch the wrong page.
func TestPaginationHint_DropsAllAndOriginalToken(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	ios.SetStderrTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	app := &App{IO: ios}

	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("page-token", "", "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().String("status", "", "")
	_ = cmd.Flags().Set("page-token", "tok_old")
	_ = cmd.Flags().Set("all", "true")
	_ = cmd.Flags().Set("status", "PROCESSED")

	pages := []any{fakePage{NextPageToken: "tok_new"}}
	rows := [][]string{{"x"}}
	if err := renderListForCmd(cmd, app, pages, []string{"id"}, rows, "none"); err != nil {
		t.Fatalf("renderListForCmd: %v", err)
	}
	got := errOut.String()
	if strings.Contains(got, "tok_old") {
		t.Errorf("hint should not echo the prior page-token; got: %q", got)
	}
	if strings.Contains(got, "--all") {
		t.Errorf("hint should drop --all when emitting an explicit page command; got: %q", got)
	}
	if !strings.Contains(got, "--status PROCESSED") {
		t.Errorf("hint should preserve other filters; got: %q", got)
	}
	if !strings.Contains(got, "--page-token tok_new") {
		t.Errorf("hint should append the new --page-token; got: %q", got)
	}
}

// TestPaginationHint_AbsentWhenNoMore confirms the hint is suppressed when
// the response signals there are no more pages.
func TestPaginationHint_AbsentWhenNoMore(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	ios.SetStderrTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: ""}}
	rows := [][]string{{"ex_1", "", ""}}
	if err := renderList(app, pages, []string{"id", "name", "created"}, rows, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if strings.Contains(errOut.String(), "more results") {
		t.Errorf("stderr should not include pagination hint, got: %q", errOut.String())
	}
}

// TestPaginationHint_AbsentWhenStderrNotTTY ensures the hint is suppressed
// when stderr is not a TTY (so script consumers don't see noise on their
// captured stderr).
func TestPaginationHint_AbsentWhenStderrNotTTY(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	// stderrTTY left at default false.
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: "tok_more"}}
	rows := [][]string{{"ex_1", "", ""}}
	if err := renderList(app, pages, []string{"id", "name", "created"}, rows, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if strings.Contains(errOut.String(), "more results") {
		t.Errorf("stderr should not include pagination hint when stderr is not a TTY, got: %q", errOut.String())
	}
}

// TestPaginationHint_FallbackWhenNoCmd verifies the legacy renderList
// codepath (no Cobra command available) still emits a useful, if less
// actionable, hint.
func TestPaginationHint_FallbackWhenNoCmd(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	ios.SetStderrTTY(true)
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: "tok_x"}}
	if err := renderList(app, pages, []string{"id"}, [][]string{{"x"}}, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	got := errOut.String()
	if !strings.Contains(got, "tok_x") {
		t.Errorf("fallback hint should mention the token; got: %q", got)
	}
	if !strings.Contains(got, "same filters") {
		t.Errorf("fallback hint should remind callers to repeat the same filters; got: %q", got)
	}
}
