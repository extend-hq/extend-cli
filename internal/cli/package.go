package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
)

// minPackageFiles and maxPackageFiles are the API's limits on the files
// of one package run, for extract and workflow runs alike.
const (
	minPackageFiles = 2
	maxPackageFiles = 50
)

// packageFlags are the --package and --files-from flags of the verbs that
// can process several files together in one run.
type packageFlags struct {
	enabled   bool
	filesFrom string
}

func (f *packageFlags) attach(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.enabled, "package", false, fmt.Sprintf("Process all the inputs (%d-%d) together in one run, with one result", minPackageFiles, maxPackageFiles))
	cmd.Flags().StringVar(&f.filesFrom, "files-from", "", "With --package, read inputs from this file, one path/URL/id per line ('-' for stdin)")
}

// inputs checks how the inputs were given and returns the package's
// inputs, or nil for an ordinary single-input run. batchCmd is the
// one-run-per-file command the errors point to.
func (f *packageFlags) inputs(verb, batchCmd string, args []string) (*packageInputs, error) {
	if f.enabled {
		return &packageInputs{args: args, filesFrom: f.filesFrom}, nil
	}
	if f.filesFrom != "" {
		return nil, fmt.Errorf("--files-from needs --package; to run each listed file on its own, use '%s --files-from'", batchCmd)
	}
	if len(args) > 1 {
		return nil, fmt.Errorf("%s takes one input, got %d: pass --package to process them together in one run, or use '%s' for one run per file", verb, len(args), batchCmd)
	}
	return nil, nil
}

// packageInputs are a package run's inputs as given: arguments, then the
// lines of a --files-from list.
type packageInputs struct {
	args      []string
	filesFrom string
}

// resolve reads the inputs and checks them against the API's limits
// before uploading anything, then uploads or resolves them in parallel,
// keeping their order.
func (p *packageInputs) resolve(ctx context.Context, app *App, cli *sdkclient.Client, password string) ([]extendx.FileRef, error) {
	inputs, err := readInputList(p.args, p.filesFrom)
	if err != nil {
		return nil, err
	}
	if len(inputs) < minPackageFiles {
		return nil, fmt.Errorf("--package needs at least %d inputs (as arguments or with --files-from), got %d", minPackageFiles, len(inputs))
	}
	if len(inputs) > maxPackageFiles {
		return nil, fmt.Errorf("--package takes at most %d inputs, got %d", maxPackageFiles, len(inputs))
	}
	// Every input is checked before any is uploaded, so a missing file
	// doesn't leave the others uploaded for nothing.
	seen := make(map[string]string, len(inputs))
	for _, in := range inputs {
		_, path, err := extendx.ResolveInput(in)
		if err != nil {
			return nil, err
		}
		key := in
		if path != "" && path != "-" {
			if abs, err := filepath.Abs(path); err == nil {
				key = abs
			}
		}
		if first, dup := seen[key]; dup {
			if first == in {
				return nil, fmt.Errorf("%s is listed twice; a package can include each file only once", in)
			}
			return nil, fmt.Errorf("%s and %s are the same file; a package can include each file only once", first, in)
		}
		seen[key] = in
	}
	return uploadAllOrResolve(ctx, app, cli, inputs, password)
}

// oneInput is the argument check for a verb that takes a single input
// and has a batch form for several. optional allows no input, for verbs
// that also take --text.
func oneInput(verb string, optional bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case len(args) > 1:
			return fmt.Errorf("%s takes one input, got %d: use 'extend %s batch' for one run per file", verb, len(args), verb)
		case len(args) == 0 && !optional:
			return fmt.Errorf("%s needs an input: a file path, file_xxx ID or https:// URL", verb)
		}
		return nil
	}
}
