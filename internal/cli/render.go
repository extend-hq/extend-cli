package cli

import (
	"fmt"
	"reflect"
	"strings"

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
func renderList(app *App, pages []any, headers []string, rows [][]string, emptyMsg string) error {
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
	maybePrintPaginationHint(app, pages)
	return nil
}

// maybePrintPaginationHint writes a dimmed line to stderr when the rendered
// list has more pages available. Conditions:
//
//   - The last page has a non-empty NextPageToken (more results exist).
//   - Stderr is a TTY (don't pollute logs/pipes for non-interactive use).
//
// This is the only hint the user gets that --all could fetch more rows; the
// JSON output already exposes nextPageToken, but the table/-o id renders
// drop it.
func maybePrintPaginationHint(app *App, pages []any) {
	if len(pages) == 0 || !app.IO.IsStderrTTY() {
		return
	}
	token := nextPageTokenOf(pages[len(pages)-1])
	if token == "" {
		return
	}
	pal := paletteFor(app.IO)
	fmt.Fprintln(app.IO.ErrOut, pal.Dimf("more results available; pass --all or filter further (next-page-token: %s)", token))
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
