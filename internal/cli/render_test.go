package cli

import (
	"strings"
	"testing"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// fakePage is the minimum shape needed to exercise renderList's pagination
// hint: a NextPageToken field for nextPageTokenOf to find via reflection.
type fakePage struct {
	NextPageToken string
}

// TestPaginationHint_AppearsOnTTYWithMore checks that the pagination hint
// fires to stderr when the last page has a NextPageToken and stderr is a
// TTY.
func TestPaginationHint_AppearsOnTTYWithMore(t *testing.T) {
	ios, _, _, errOut := iostreams.Test()
	ios.SetStderrTTY(true)
	ios.SetStdoutTTY(true)
	ios.SetColorEnabled(false)
	app := &App{IO: ios}

	pages := []any{fakePage{NextPageToken: "tok_more"}}
	rows := [][]string{{"ex_1", "one", ""}}
	if err := renderList(app, pages, []string{"id", "name", "created"}, rows, "none"); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if !strings.Contains(errOut.String(), "more results available") {
		t.Errorf("stderr missing pagination hint, got: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "tok_more") {
		t.Errorf("stderr should include the next-page-token; got: %q", errOut.String())
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
