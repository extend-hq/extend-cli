package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/extend-hq/extend-cli/internal/output"
)

func renderWithDefault(app *App, payload any, defaultFormat output.Format) error {
	f := defaultFormat
	if app.Format != "" {
		parsed, err := output.ParseFormat(app.Format)
		if err != nil {
			return err
		}
		f = parsed
	}
	return output.Render(app.IO.Out, f, payload,
		output.WithJQ(app.JQ),
		output.WithPretty(app.IO.IsStdoutTTY()),
	)
}

// renderList picks the right renderer for a paginated list result based on
// --output:
//
//	-o table        -> tabwriter table (regardless of TTY)
//	-o markdown     -> GitHub-flavored markdown pipe table
//	-o json/yaml/raw/id -> falls through to renderWithDefault on the raw payload
//	no -o, TTY      -> tabwriter table (preserves the historical default)
//	no -o, non-TTY  -> JSON (script-friendly default)
//
// pages is the slice of API page objects collected by the list command;
// it is unwrapped to the single page when len==1 so json/yaml output for a
// non-paginated call doesn't get an outer array wrapper. emptyMsg is shown
// instead of a header-only table when rows is empty and a tabular format
// was selected.
// renderList is the legacy entry point that has no access to the invoking
// Cobra command, so it cannot build a fully-reusable next-page command in
// the pagination hint. Prefer renderListForCmd from RunE callbacks.
func renderList(app *App, pages []any, headers []string, rows [][]string, emptyMsg string) error {
	return renderListForCmd(nil, app, pages, headers, rows, emptyMsg)
}

// renderListForCmd renders a paginated list and prints a smart pagination
// hint to stderr when more pages are available. cmd is used to compose the
// next-page command line: page tokens are bound to the originating query's
// filters server-side, so the hint must include every user-set flag from
// the current invocation (with --page-token swapped for the new token).
//
// Pass cmd=nil to suppress the smart hint and fall back to a token-only
// notice; this preserves the renderList contract for callers that don't
// have a cmd reference handy.
func renderListForCmd(cmd *cobra.Command, app *App, pages []any, headers []string, rows [][]string, emptyMsg string) error {
	var raw any = pages
	if len(pages) == 1 {
		raw = pages[0]
	}

	var renderErr error
	switch {
	case app.Format == "":
		switch {
		case app.JQ != "":
			renderErr = renderWithDefault(app, raw, output.FormatJSON)
		case app.IO.IsStdoutTTY():
			renderErr = renderTableOrEmpty(app, headers, rows, emptyMsg)
		default:
			renderErr = renderWithDefault(app, raw, output.FormatJSON)
		}
	default:
		parsed, err := output.ParseFormat(app.Format)
		if err != nil {
			return err
		}
		switch parsed {
		case output.FormatTable:
			renderErr = renderTableOrEmpty(app, headers, rows, emptyMsg)
		case output.FormatMarkdown:
			if len(rows) == 0 {
				fmt.Fprintln(app.IO.Out, emptyMsg)
			} else {
				renderErr = output.RenderMarkdownTable(app.IO.Out, headers, rows)
			}
		case output.FormatID:
			if app.JQ != "" {
				renderErr = renderWithDefault(app, raw, output.FormatJSON)
				break
			}
			// `-o id` on a list emits one ID per row so the result composes
			// with xargs / shell pipes. We use the rows + headers already
			// built for the table: locate the "id" column case-insensitively
			// (some lists, e.g. processor versions, put id in column 1, not
			// 0). If the list has no id column we fall through to
			// renderWithDefault so the user gets the same error they would
			// have hit before.
			if idx := indexOfHeader(headers, "id"); idx >= 0 {
				for _, row := range rows {
					if idx < len(row) {
						fmt.Fprintln(app.IO.Out, row[idx])
					}
				}
				break
			}
			renderErr = renderWithDefault(app, raw, output.FormatJSON)
		default:
			renderErr = renderWithDefault(app, raw, output.FormatJSON)
		}
	}
	if renderErr != nil {
		return renderErr
	}
	maybePrintPaginationHint(cmd, app, pages)
	return nil
}

// maybePrintPaginationHint writes a dimmed line to stderr when the rendered
// list has more pages available. Conditions:
//
//   - The last page has a non-empty NextPageToken (more results exist).
//   - Stderr is a TTY (don't pollute logs/pipes for non-interactive use).
//
// The hint deliberately points at --page-token rather than --all: agent
// callers iterating through pages should drive pagination explicitly so
// they can decide when to stop. --all exists, but encouraging it as the
// default tool produces unbounded-context calls.
//
// When cmd is non-nil, the hint includes the full reusable command (with
// every user-set flag from the current invocation), because page tokens
// are bound server-side to the originating query: changing filters between
// pages produces incorrect results.
func maybePrintPaginationHint(cmd *cobra.Command, app *App, pages []any) {
	if len(pages) == 0 || !app.IO.IsStderrTTY() {
		return
	}
	token := nextPageTokenOf(pages[len(pages)-1])
	if token == "" {
		return
	}
	pal := paletteFor(app.IO)
	if cmd == nil {
		fmt.Fprintln(app.IO.ErrOut, pal.Dimf("more results available; pass --page-token %s with the same filters as this call", token))
		return
	}
	fmt.Fprintln(app.IO.ErrOut, pal.Dimf("more results available; same filters, next page:"))
	fmt.Fprintln(app.IO.ErrOut, pal.Dimf("  %s", nextPageCommand(cmd, token)))
}

// nextPageCommand composes a runnable shell line that fetches the next
// page. It rebuilds the original invocation by walking every user-set
// flag and adds (or replaces) --page-token with the new value. Page-tuning
// flags that don't survive across pages (--all) are dropped.
func nextPageCommand(cmd *cobra.Command, token string) string {
	parts := []string{cmd.CommandPath()}
	cmd.Flags().Visit(func(f *pflag.Flag) {
		switch f.Name {
		case "page-token", "all":
			// page-token is replaced below; --all is meaningless when
			// driving pagination explicitly.
			return
		}
		parts = append(parts, formatFlagForShell(f))
	})
	parts = append(parts, "--page-token", shellQuote(token))
	return strings.Join(parts, " ")
}

// formatFlagForShell renders a user-set flag as it would appear on the
// command line, quoting the value when it contains shell-significant
// characters. Repeated string-array flags emit one --flag value pair per
// element so the rendered line is a faithful re-invocation.
func formatFlagForShell(f *pflag.Flag) string {
	if sa, ok := f.Value.(pflag.SliceValue); ok {
		var b strings.Builder
		for i, v := range sa.GetSlice() {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "--%s %s", f.Name, shellQuote(v))
		}
		return b.String()
	}
	val := f.Value.String()
	switch f.Value.Type() {
	case "bool":
		if val == "true" {
			return "--" + f.Name
		}
		return fmt.Sprintf("--%s=false", f.Name)
	}
	return fmt.Sprintf("--%s %s", f.Name, shellQuote(val))
}

// shellQuote wraps s in single quotes if it contains characters that would
// otherwise be interpreted by a POSIX shell. Bare alphanumeric+/-/_/./
// values pass through unquoted so the rendered command stays readable.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || r == ',' || r == '+':
			return false
		}
		return true
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// nextPageTokenOf extracts a NextPageToken string field from a struct (or
// pointer to struct) using reflection. Returns "" when the field is absent
// or empty. Used by the pagination hint so a single check works across
// every list response type.
func nextPageTokenOf(page any) string {
	if page == nil {
		return ""
	}
	v := reflect.ValueOf(page)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName("NextPageToken")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

func indexOfHeader(headers []string, name string) int {
	for i, h := range headers {
		if strings.EqualFold(h, name) {
			return i
		}
	}
	return -1
}

func renderTableOrEmpty(app *App, headers []string, rows [][]string, emptyMsg string) error {
	if len(rows) == 0 {
		fmt.Fprintln(app.IO.Out, emptyMsg)
		return nil
	}
	return output.RenderTable(app.IO.Out, headers, rows)
}
