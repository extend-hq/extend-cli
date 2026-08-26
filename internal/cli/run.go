package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	extend "github.com/extend-hq/extend-go-sdk"
	sdkclient "github.com/extend-hq/extend-go-sdk/client"

	"github.com/extend-hq/extend-cli/internal/extendx"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newWorkflowsRunDoc returns the typed documentation for `extend
// workflows run` (the workflow-run launcher) and its `extend workflows
// run batch` subcommand.
func newWorkflowsRunDoc(app *App) *CommandDoc {
	var (
		workflowID  string
		version     string
		wait        bool
		priority    int
		timeout     time.Duration
		outputsPath string
		secrets     []string
		password    string
		text        string
		name        string
		meta        metaFlags
	)

	return &CommandDoc{
		Use:     "run <input>",
		Summary: "Start a workflow run on a document",
		Triggers: []string{
			"start a workflow run on a document",
			"trigger an extend workflow on a pdf",
			"submit a multi-step pipeline run",
			"kick off a custom processing workflow",
			"run an extend workflow against a file",
		},
		WhenToUse: `Use when you have a configured Extend workflow (multi-step pipeline) and
want to run it on a document. Prefer the single-purpose verbs (extract,
classify, split, parse) when you only need one processor.`,
		Details: `<input> can be:
  - a local file path (auto-uploaded)
  - a file_xxx ID (use a previously uploaded file)
  - an https:// URL (Extend fetches the document)
  - omitted, with --text "<inline text>" to run the workflow on raw text directly

Workflow runs are asynchronous by default because they can take minutes to
hours; the run ID and dashboard URL are printed immediately.

Use --version to run "latest", "draft", or a named workflow deploy created
with extend workflows versions create --name.

Use --wait to block until the run reaches a terminal state. NEEDS_REVIEW
is treated as terminal because it pauses for human action; use the
dashboard URL to review and approve.

--outputs lets a caller seed the run with pre-computed processor outputs
(skips the matching steps). Source: inline JSON, a plain path, an absolute
file:// URI, or '-' to read from stdin. Each entry is {processorId,
output}; output is the same shape that processor would normally return
(extract: {value}, classify: {id, type, confidence}, split: {splits[]}).

--secret key=value provides per-run secrets that step actions can reference.
Repeatable.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend workflows run invoice.pdf --using workflow_abc"},
			{Label: "Block until terminal", Cmd: "extend workflows run invoice.pdf --using workflow_abc --wait"},
			{Label: "Pin version with priority", Cmd: "extend workflows run invoice.pdf --using workflow_abc --version v2-with-review --priority 10"},
			{Label: "Seed processor outputs", Cmd: "extend workflows run invoice.pdf --using workflow_abc --outputs seeded.json"},
			{Label: "Inline seeded outputs", Cmd: `extend workflows run invoice.pdf --using workflow_abc --outputs '[{"processorId":"ex_abc","output":{"value":{}}}]`},
			{Label: "Pass a secret", Cmd: "extend workflows run invoice.pdf --using workflow_abc --secret API_KEY=$KEY"},
		},
		Gotchas: []string{
			"Workflow runs are async by default (unlike extract/classify/split). Pass --wait to block.",
			"NEEDS_REVIEW is treated as terminal — review and approve at the dashboard URL.",
			"--outputs entries must match the shape of the processor they replace (extract/classify/split).",
			"--secret values are not echoed back; use them for per-run API keys, not as run metadata.",
		},
		SeeAlso:  []string{"extract", "workflows run batch", "workflows runs watch", "workflows runs get", "webhooks subscriptions create"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: extendx.ProfileLong, DefaultsToWait: false},
		Failures: []extendx.RunStatus{extendx.StatusFailed, extendx.StatusCancelled, extendx.StatusRejected},
		Args:     cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			input := ""
			if len(args) > 0 {
				input = args[0]
			}
			return runWorkflow(cmd.Context(), app, workflowParams{
				input:       input,
				text:        text,
				name:        name,
				workflowID:  workflowID,
				version:     version,
				wait:        wait,
				priority:    priority,
				timeout:     timeout,
				outputsPath: outputsPath,
				secrets:     secrets,
				password:    password,
				metadata:    md,
			})
		},
		Configure: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&workflowID, "using", "", "Workflow ID (required, e.g. workflow_xxx)")
			cmd.Flags().StringVar(&version, "version", "", "Workflow version: latest, draft, or named deploy (e.g. v2-with-review)")
			cmd.Flags().BoolVar(&wait, "wait", false, "Block until the run reaches a terminal state")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 1*time.Hour, "Maximum total time to wait for the run to reach a terminal state when --wait is set (not a per-HTTP-request timeout; see --http-timeout)")
			cmd.Flags().StringVar(&outputsPath, "outputs", "", "Pre-computed [{processorId, output}] entries that seed the run and skip matching steps. Source: inline JSON, path, file:// URI, or '-' for stdin.")
			cmd.Flags().StringArrayVar(&secrets, "secret", nil, "key=value secret available to step actions (repeatable)")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			cmd.Flags().StringVar(&text, "text", "", "Run the workflow on this inline text instead of a file/URL/ID input (omit the positional <input>)")
			cmd.Flags().StringVar(&name, "name", "", "Display name for the input (honored for --text and URL inputs)")
			meta.attach(cmd)
			_ = cmd.MarkFlagRequired("using")
		},
		Subcommands: []*CommandDoc{newWorkflowBatchDoc(app)},
	}
}

type workflowParams struct {
	input       string
	text        string
	name        string
	workflowID  string
	version     string
	wait        bool
	priority    int
	timeout     time.Duration
	outputsPath string
	secrets     []string
	password    string
	metadata    map[string]any
}

func runWorkflow(ctx context.Context, app *App, p workflowParams) error {
	cli, err := app.NewClient()
	if err != nil {
		return err
	}

	ref, err := resolveInputOrText(ctx, app, cli, p.input, p.text, p.name, p.password)
	if err != nil {
		return err
	}

	file, err := extendx.BuildWorkflowFile(ref)
	if err != nil {
		return err
	}

	wf := &extend.WorkflowReference{ID: p.workflowID}
	if p.version != "" {
		v := extend.ProcessorVersionString(p.version)
		wf.Version = &v
	}

	req := &extend.WorkflowRunsCreateRequest{
		Workflow: wf,
		File:     file,
	}
	if p.metadata != nil {
		md := extend.RunMetadata(p.metadata)
		req.Metadata = &md
	}
	if p.priority > 0 {
		pr := extend.RunPriority(p.priority)
		req.Priority = &pr
	}
	if p.outputsPath != "" {
		raw, err := readJSONFile(p.outputsPath, "--outputs")
		if err != nil {
			return err
		}
		var outputs []*extend.WorkflowRunsCreateRequestOutputsItem
		if err := json.Unmarshal(raw, &outputs); err != nil {
			return fmt.Errorf("--outputs: %w (expected JSON array of {processorId, output})", err)
		}
		req.Outputs = outputs
	}
	if len(p.secrets) > 0 {
		pairs, err := parseKVPairs("--secret", p.secrets)
		if err != nil {
			return err
		}
		secrets := make(extend.RunSecrets, len(pairs))
		for k, v := range pairs {
			secrets[k] = v
		}
		req.Secrets = &secrets
	}

	run, err := cli.WorkflowRuns.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWorkflowResult(app, run)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := waitForWorkflowRun(ctx, cli, run.ID, extendx.WaitProfileOptions(extendx.ProfileLong, p.timeout), func(r *extend.WorkflowRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return formatActionWaitError(err, run.ID, "extend workflows runs watch")
	}

	if err := renderWorkflowResult(app, final); err != nil {
		return err
	}
	switch extendx.RunStatus(final.Status) {
	case extendx.StatusFailed:
		return runFailureError(final.ID, final.FailureReason, final.FailureMessage)
	case extendx.StatusCancelled:
		return fmt.Errorf("run %s was cancelled", final.ID)
	case extendx.StatusRejected:
		return fmt.Errorf("run %s was rejected", final.ID)
	}
	return nil
}

func waitForWorkflowRun(ctx context.Context, c *sdkclient.Client, id string, opts extendx.WaitOptions, onPoll func(*extend.WorkflowRun)) (*extend.WorkflowRun, error) {
	return extendx.PollForRun(ctx,
		func(ctx context.Context) (*extend.WorkflowRun, error) {
			return c.WorkflowRuns.Retrieve(ctx, id, &extend.WorkflowRunsRetrieveRequest{})
		},
		func(r *extend.WorkflowRun) extendx.RunStatus { return extendx.RunStatus(r.Status) },
		opts, onPoll,
	)
}

func renderWorkflowResult(app *App, run *extend.WorkflowRun) error {
	if app.Format != "" || app.JQ != "" {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	return renderWorkflowTTY(app, run)
}

func renderWorkflowTTY(app *App, run *extend.WorkflowRun) error {
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s, %d step%s)\n",
		statusIcon(pal, extendx.RunStatus(run.Status)), run.ID, run.Status, len(run.StepRuns), pluralize(len(run.StepRuns)))
	if run.DashboardURL != "" {
		fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Dashboard: %s", run.DashboardURL))
	}
	if extendx.RunStatus(run.Status) == extendx.StatusNeedsReview {
		fmt.Fprintln(app.IO.Out, "  Awaiting human review at the dashboard URL above.")
	}
	if len(run.StepRuns) > 0 {
		fmt.Fprintln(app.IO.Out)
		rows := make([][]string, 0, len(run.StepRuns))
		for i, step := range run.StepRuns {
			name, stepType, status := stepRunInfo(step)
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				name,
				stepType,
				status,
			})
		}
		return output.RenderTable(app.IO.Out, []string{"step", "name", "type", "status"}, rows)
	}
	return nil
}

// stepRunInfo flattens the SDK's discriminated-union *extend.StepRun
// into a (name, type, status) triple suitable for table rendering.
// The union has one variant per workflow step type; each variant has
// its own typed Step struct, but they all expose Name/Type fields with
// the same names. We dispatch on StepType and reach into the matching
// branch.
func stepRunInfo(step *extend.StepRun) (name, stepType, status string) {
	if step == nil {
		return "", "", ""
	}
	// Each variant exposes the same {Status, Step.{Name, Type}} shape
	// but with kind-specific concrete types, so we can't iterate the
	// union; we route every case through a single capture closure
	// that takes the already-resolved fields.
	capture := func(st extend.StepRunBaseStatus, n string, t *string) {
		status = string(st)
		name = n
		if t != nil {
			stepType = *t
		}
	}
	switch step.StepType {
	case "PARSE":
		if s := step.Parse; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "EXTRACT":
		if s := step.Extract; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "CLASSIFY":
		if s := step.Classify; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "SPLIT":
		if s := step.Split; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "MERGE_EXTRACT":
		if s := step.MergeExtract; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "CONDITIONAL_EXTRACT":
		if s := step.ConditionalExtract; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "RULE_VALIDATION":
		if s := step.RuleValidation; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	case "EXTERNAL_DATA_VALIDATION":
		if s := step.ExternalDataValidation; s != nil && s.Step != nil {
			capture(s.Status, s.Step.Name, s.Step.Type)
		} else if s != nil {
			status = string(s.Status)
		}
	}
	if stepType == "" {
		stepType = step.StepType
	}
	return name, stepType, status
}

func statusIcon(p palette, s extendx.RunStatus) string {
	switch s {
	case extendx.StatusProcessed:
		return p.Green("✓")
	case extendx.StatusFailed, extendx.StatusRejected:
		return p.Red("✗")
	case extendx.StatusCancelled, extendx.StatusCancelling:
		return p.Dim("○")
	case extendx.StatusNeedsReview:
		return p.Yellow("⏸")
	case extendx.StatusPending, extendx.StatusProcessing:
		return p.Cyan("⋯")
	}
	return "•"
}

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
