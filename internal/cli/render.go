package cli

import (
	"fmt"
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

	if app.Format == "" {
		if app.JQ != "" {
			return renderWithDefault(app, raw, output.FormatJSON)
		}
		if app.IO.IsStdoutTTY() {
			return renderTableOrEmpty(app, headers, rows, emptyMsg)
		}
		return renderWithDefault(app, raw, output.FormatJSON)
	}

	parsed, err := output.ParseFormat(app.Format)
	if err != nil {
		return err
	}
	switch parsed {
	case output.FormatTable:
		return renderTableOrEmpty(app, headers, rows, emptyMsg)
	case output.FormatMarkdown:
		if len(rows) == 0 {
			fmt.Fprintln(app.IO.Out, emptyMsg)
			return nil
		}
		return output.RenderMarkdownTable(app.IO.Out, headers, rows)
	case output.FormatID:
		if app.JQ != "" {
			return renderWithDefault(app, raw, output.FormatJSON)
		}
		// `-o id` on a list emits one ID per row so the result composes with
		// xargs / shell pipes. We use the rows + headers we already built for
		// the table: locate the "id" column case-insensitively (some lists,
		// e.g. processor versions, put id in column 1, not 0). If the list
		// has no id column we fall through to renderWithDefault so the user
		// gets the same error they would have hit before.
		if idx := indexOfHeader(headers, "id"); idx >= 0 {
			for _, row := range rows {
				if idx < len(row) {
					fmt.Fprintln(app.IO.Out, row[idx])
				}
			}
			return nil
		}
		return renderWithDefault(app, raw, output.FormatJSON)
	default:
		return renderWithDefault(app, raw, output.FormatJSON)
	}
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
