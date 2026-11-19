package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/extend-hq/extend-cli/internal/iostreams"
)

// runRoot runs the CLI as main does, root command and all, against srv,
// with stdout a terminal or not, and returns what it printed there.
func runRoot(t *testing.T, srv *fakeServer, tty bool, args ...string) string {
	t.Helper()
	t.Setenv("EXTEND_API_KEY", "test-key")
	t.Setenv("EXTEND_BASE_URL", srv.URL())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ios, _, out, _ := iostreams.Test()
	ios.SetStdoutTTY(tty)
	ios.SetColorEnabled(false)
	root := newRoot(ios)
	root.SetArgs(args)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("extend %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func pipedOutputServer(t *testing.T) *fakeServer {
	t.Helper()
	return newFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/extractors":
			writeJSON(w, 200, map[string]any{"object": "list", "data": []map[string]any{
				{"id": "ex_1", "name": "Invoices", "createdAt": "2026-11-06T10:00:00Z", "updatedAt": "2026-11-06T10:00:00Z"},
			}})
		case strings.HasPrefix(r.URL.Path, "/edit_runs"):
			writeJSON(w, 200, map[string]any{"id": "edr_x", "status": "PROCESSED"})
		case r.URL.Path == "/parse_runs":
			writeJSON(w, 200, map[string]any{"id": "pr_md", "status": "PENDING"})
		case r.URL.Path == "/parse_runs/pr_md":
			writeJSON(w, 200, map[string]any{"id": "pr_md", "status": "PROCESSED", "output": map[string]any{
				"chunks": []map[string]any{{"object": "chunk", "type": "page", "content": "# Hello", "blocks": []any{}}},
			}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
}

// With no format chosen, a command whose piped default is JSON prints a
// table or summary on a terminal and JSON everywhere else, so a script or
// an agent can parse the output without asking for it.
func TestPipedOutputDefaultsToJSON(t *testing.T) {
	srv := pipedOutputServer(t)
	t.Setenv("EXTEND_OUTPUT", "")

	piped := runRoot(t, srv, false, "extractors", "list")
	var page struct {
		Data []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(piped), &page); err != nil || len(page.Data) != 1 || page.Data[0].ID != "ex_1" {
		t.Errorf("piped list = %q (%v), want JSON with ex_1", piped, err)
	}
	if strings.Count(strings.TrimSpace(piped), "\n") != 0 {
		t.Errorf("piped JSON = %q, want it on one line", piped)
	}
	if onTerminal := runRoot(t, srv, true, "extractors", "list"); !strings.HasPrefix(onTerminal, "ID ") || !strings.Contains(onTerminal, "ex_1") {
		t.Errorf("list on a terminal = %q, want the table", onTerminal)
	}

	var run struct{ ID, Status string }
	if piped := runRoot(t, srv, false, "edit", "file_a", "--instructions", "x"); json.Unmarshal([]byte(piped), &run) != nil || run.ID != "edr_x" {
		t.Errorf("piped edit = %q, want the run as JSON", piped)
	}
	if onTerminal := runRoot(t, srv, true, "edit", "file_a", "--instructions", "x"); !strings.Contains(onTerminal, "edr_x (PROCESSED") {
		t.Errorf("edit on a terminal = %q, want the summary", onTerminal)
	}

	if got := runRoot(t, srv, false, "extractors", "list", "--jq", ".data[0].id"); strings.TrimSpace(got) != `"ex_1"` {
		t.Errorf("piped list with --jq = %q, want \"ex_1\"", got)
	}
}

// A format the caller chose wins over the piped default.
func TestPipedOutputKeepsAChosenFormat(t *testing.T) {
	srv := pipedOutputServer(t)
	t.Setenv("EXTEND_OUTPUT", "")
	if got := runRoot(t, srv, false, "extractors", "list", "-o", "table"); !strings.HasPrefix(got, "ID ") {
		t.Errorf("piped list with -o table = %q, want the table", got)
	}
	t.Setenv("EXTEND_OUTPUT", "table")
	if got := runRoot(t, srv, false, "extractors", "list"); !strings.HasPrefix(got, "ID ") {
		t.Errorf("piped list with EXTEND_OUTPUT=table = %q, want the table", got)
	}
}

// parse prints the document's markdown whether piped or not: piping it
// to a file or another tool is what it's for.
func TestPipedParseStillPrintsMarkdown(t *testing.T) {
	srv := pipedOutputServer(t)
	t.Setenv("EXTEND_OUTPUT", "")
	if got := runRoot(t, srv, false, "parse", "file_a"); got != "# Hello\n" {
		t.Errorf("piped parse = %q, want the markdown", got)
	}
}
