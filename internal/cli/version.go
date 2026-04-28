package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/version"
)

func newVersionCommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version, commit, and build platform",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(app.IO.Out, version.String())
			return nil
		},
	}
}
