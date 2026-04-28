package cli

import (
	"fmt"

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
	default:
		return renderWithDefault(app, raw, output.FormatJSON)
	}
}

func renderTableOrEmpty(app *App, headers []string, rows [][]string, emptyMsg string) error {
	if len(rows) == 0 {
		fmt.Fprintln(app.IO.Out, emptyMsg)
		return nil
	}
	return output.RenderTable(app.IO.Out, headers, rows)
}
