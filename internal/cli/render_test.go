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
