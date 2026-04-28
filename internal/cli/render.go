package cli

import (
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
