package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// This file holds the typed `extend <verb> batches` inspection
// subgroups (get/watch on already-submitted batches). The batch
// *submit* builders (`extend <verb> batch`) live in batch.go with
// their upload plumbing.

// batchesGroupSpec parameterizes the generated `extend <verb> batches`
// subgroup. Only verbs whose batch endpoint supports GET
// /batch_runs/{id} attach one: extract, classify, and split share the
// processor batch kind (bpr_), parse has its own (bpar_). Workflow
// batches have no retrieval endpoint, so `workflows` has no batches
// group.
type batchesGroupSpec struct {
	verb      string
	batchKind extendx.BatchKind
	exampleID string
}

func extractBatchesSpec() batchesGroupSpec {
	return batchesGroupSpec{verb: "extract", batchKind: extendx.BatchKindProcessor, exampleID: "bpr_xK9mLPq"}
}

func classifyBatchesSpec() batchesGroupSpec {
	return batchesGroupSpec{verb: "classify", batchKind: extendx.BatchKindProcessor, exampleID: "bpr_kMXkR"}
}

func splitBatchesSpec() batchesGroupSpec {
	return batchesGroupSpec{verb: "split", batchKind: extendx.BatchKindProcessor, exampleID: "bpr_s8Yqw"}
}

func parseBatchesSpec() batchesGroupSpec {
	return batchesGroupSpec{verb: "parse", batchKind: extendx.BatchKindParse, exampleID: "bpar_pJDa8"}
}

func (s batchesGroupSpec) prefix() string {
	if s.batchKind == extendx.BatchKindParse {
		return "bpar_"
	}
	return "bpr_"
}

// sharedPrefixNote documents the bpr_ ambiguity for the processor
// verbs: prefix validation alone cannot tell an extract batch from a
// classify or split batch, so the server resolves the exact type.
func (s batchesGroupSpec) sharedPrefixNote() string {
	if s.batchKind != extendx.BatchKindProcessor {
		return ""
	}
	return "The bpr_ prefix is shared by extract, classify, and split batches; the server resolves the exact type."
}

// doc returns the typed documentation tree for `extend <verb> batches`.
func (s batchesGroupSpec) doc(app *App) *CommandDoc {
	return &CommandDoc{
		Use:     "batches",
		Summary: fmt.Sprintf("Inspect and follow %s batch runs", s.verb),
		WhenToUse: fmt.Sprintf(`Use these commands to inspect or watch a %s batch run (submitted with
'extend %s batch') by its %s ID.`, s.verb, s.verb, s.prefix()),
		Details: fmt.Sprintf(`Operations on %s batch runs identified by their %s ID.`, s.verb, s.prefix()),
		Subcommands: []*CommandDoc{
			s.getDoc(app),
			s.watchDoc(app),
		},
	}
}

func (s batchesGroupSpec) getDoc(app *App) *CommandDoc {
	gotchas := []string{
		fmt.Sprintf("This command never waits; use 'extend %s batches watch' for live polling.", s.verb),
	}
	if n := s.sharedPrefixNote(); n != "" {
		gotchas = append(gotchas, n)
	}
	return &CommandDoc{
		Use:     "get <batch-id>",
		Summary: fmt.Sprintf("Show one %s batch run by ID", s.verb),
		Triggers: []string{
			fmt.Sprintf("show one %s batch run", s.verb),
			fmt.Sprintf("inspect a %s batch by id", s.verb),
			fmt.Sprintf("check %s batch status", s.verb),
		},
		WhenToUse: fmt.Sprintf(`Use to retrieve the current status, member-run count, and timestamps
for a single %s batch run. Does not poll; for live progress use
'extend %s batches watch'.`, s.verb, s.verb),
		Details: fmt.Sprintf(`Show one %s batch run, including its overall status, member-run count,
and timestamps. To list the individual runs inside the batch, use
'extend %s runs list --batch <id>'.`, s.verb, s.verb),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s batches get %s", s.verb, s.exampleID)},
			{Label: "Just the status", Cmd: fmt.Sprintf("extend %s batches get %s --jq '.status' -o raw", s.verb, s.exampleID)},
		},
		Gotchas: gotchas,
		SeeAlso: []string{s.verb + " batches watch", s.verb + " runs list"},
		Output:  OutputSpec{TTY: OutputJSON, Pipe: OutputJSON},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := extendx.ValidateBatchID(s.batchKind, id, "get"); err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			br, err := cli.BatchRuns.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			return renderWithDefault(app, br, output.FormatJSON)
		},
	}
}

func (s batchesGroupSpec) watchDoc(app *App) *CommandDoc {
	var (
		timeout    time.Duration
		exitStatus bool
	)
	gotchas := []string{
		"Without --exit-status, the command exits 0 on terminal regardless of FAILED/CANCELLED.",
	}
	if n := s.sharedPrefixNote(); n != "" {
		gotchas = append(gotchas, n)
	}
	return &CommandDoc{
		Use:     "watch <batch-id>",
		Summary: fmt.Sprintf("Poll %s %s batch until it reaches a terminal state", articleFor(s.verb), s.verb),
		Triggers: []string{
			fmt.Sprintf("watch %s %s batch until it finishes", articleFor(s.verb), s.verb),
			fmt.Sprintf("poll %s %s batch run", articleFor(s.verb), s.verb),
			fmt.Sprintf("block until %s %s batch completes", articleFor(s.verb), s.verb),
		},
		WhenToUse: fmt.Sprintf(`Use to block until %s %s batch reaches a terminal state. Combine with
--exit-status to gate downstream scripts on success.`, articleFor(s.verb), s.verb),
		Details: fmt.Sprintf(`Poll %s %s batch and print the final status when it reaches a terminal
state.

Pass --exit-status to make the command exit non-zero when the batch
finishes in FAILED or CANCELLED status, suitable for shell composition:

    extend %s batches watch %s --exit-status && downstream-script.sh

Polls every 2s, backing off to 30s.`, articleFor(s.verb), s.verb, s.verb, s.exampleID),
		Examples: []Example{
			{Label: "Basic", Cmd: fmt.Sprintf("extend %s batches watch %s", s.verb, s.exampleID)},
			{Label: "Custom timeout", Cmd: fmt.Sprintf("extend %s batches watch %s --timeout 2h", s.verb, s.exampleID)},
			{Label: "Gate downstream script", Cmd: fmt.Sprintf("extend %s batches watch %s --exit-status", s.verb, s.exampleID)},
		},
		Gotchas:  gotchas,
		SeeAlso:  []string{s.verb + " batches get", s.verb + " runs list", s.verb + " runs watch"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileLong, DefaultsToWait: true},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := extendx.ValidateBatchID(s.batchKind, id, "watch"); err != nil {
				return err
			}
			cli, err := app.NewClient()
			if err != nil {
				return err
			}
			sp := app.IO.StartSpinner(fmt.Sprintf("Batch %s: ?", id))
			final, err := waitForBatchRun(cmd.Context(), cli, id, extendx.WaitProfileOptions(extendx.ProfileLong, timeout), func(r *extend.BatchRun) {
				sp.Update(fmt.Sprintf("Batch %s: %s (%d run%s)", r.ID, r.Status, r.RunCount, pluralize(r.RunCount)))
			})
			sp.Stop("")
			if err != nil {
				return formatWatchWaitError(err, id, fmt.Sprintf("extend %s batches watch", s.verb))
			}
			if err := renderBatchSubmitted(app, final, s.verb); err != nil {
				return err
			}
			if exitStatus {
				switch extendx.RunStatus(final.Status) {
				case extendx.StatusFailed:
					return fmt.Errorf("batch %s failed", id)
				case extendx.StatusCancelled:
					return fmt.Errorf("batch %s was cancelled", id)
				}
			}
			return nil
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().DurationVar(&timeout, "timeout", 1*time.Hour, "Maximum total time to wait for the batch to reach a terminal state (not a per-HTTP-request timeout; see --http-timeout)")
			cmd.Flags().BoolVar(&exitStatus, "exit-status", false, "Exit non-zero on FAILED or CANCELLED")
		},
	}
}

func waitForBatchRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.BatchRun)) (*extend.BatchRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.BatchRun, error) {
			return c.BatchRuns.Get(ctx, id)
		},
		func(r *extend.BatchRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}
