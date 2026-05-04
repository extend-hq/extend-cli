package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/extend-hq/extend-cli/internal/client"
	"github.com/extend-hq/extend-cli/internal/output"
)

// newRunDoc returns the typed documentation for `extend run` (the
// workflow-run launcher) and its `extend run batch` subcommand.
func newRunDoc(app *App) *CommandDoc {
	var (
		workflowID  string
		version     string
		wait        bool
		priority    int
		timeout     time.Duration
		outputsPath string
		secrets     []string
		password    string
		meta        metaFlags
	)

	return &CommandDoc{
		Use:     "run <input>",
		Summary: "Start a workflow run on a document",
		Group:   "Actions",
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

Workflow runs are asynchronous by default because they can take minutes to
hours; the run ID and dashboard URL are printed immediately.

Use --wait to block until the run reaches a terminal state. NEEDS_REVIEW
is treated as terminal because it pauses for human action; use the
dashboard URL to review and approve.

--outputs lets a caller seed the run with pre-computed processor outputs
(skips the matching steps). Pass a JSON array inline, as a plain path, or as
an absolute file:// URI. Each entry is {processorId, output}; output is the
same shape that processor would normally return (extract: {value}, classify:
{id, type, confidence}, split: {splits[]}).

--secret key=value provides per-run secrets that step actions can reference.
Repeatable.`,
		Examples: []Example{
			{Label: "Basic", Cmd: "extend run invoice.pdf --workflow workflow_abc"},
			{Label: "Block until terminal", Cmd: "extend run invoice.pdf --workflow workflow_abc --wait"},
			{Label: "Pin version with priority", Cmd: "extend run invoice.pdf --workflow workflow_abc --version 3 --priority 10"},
			{Label: "Seed processor outputs", Cmd: "extend run invoice.pdf --workflow workflow_abc --outputs seeded.json"},
			{Label: "Inline seeded outputs", Cmd: `extend run invoice.pdf --workflow workflow_abc --outputs '[{"processorId":"ex_abc","output":{"value":{}}}]`},
			{Label: "Pass a secret", Cmd: "extend run invoice.pdf --workflow workflow_abc --secret API_KEY=$KEY"},
		},
		Gotchas: []string{
			"Workflow runs are async by default (unlike extract/classify/split). Pass --wait to block.",
			"NEEDS_REVIEW is treated as terminal — review and approve at the dashboard URL.",
			"--outputs entries must match the shape of the processor they replace (extract/classify/split).",
			"--secret values are not echoed back; use them for per-run API keys, not as run metadata.",
		},
		SeeAlso:  []string{"extract", "run batch", "runs watch", "runs get", "webhooks subscriptions create"},
		Output:   OutputSpec{TTY: OutputPretty, Pipe: OutputJSON},
		Wait:     &WaitSpec{Profile: client.ProfileLong, DefaultsToWait: false},
		Failures: []client.RunStatus{client.StatusFailed, client.StatusCancelled, client.StatusRejected},
		Args:     cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			md, err := meta.build()
			if err != nil {
				return err
			}
			return runWorkflow(cmd.Context(), app, workflowParams{
				input:       args[0],
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
			cmd.Flags().StringVar(&workflowID, "workflow", "", "Workflow ID (required)")
			cmd.Flags().StringVar(&version, "version", "", "Workflow version: latest, draft, or specific (e.g. 3)")
			cmd.Flags().BoolVar(&wait, "wait", false, "Block until the run reaches a terminal state")
			cmd.Flags().IntVar(&priority, "priority", 0, "Priority 0-100 (lower = higher priority); 0 = default")
			cmd.Flags().DurationVar(&timeout, "timeout", 1*time.Hour, "Maximum time to wait when --wait is set")
			cmd.Flags().StringVar(&outputsPath, "outputs", "", "JSON array, path, or file:// URI for pre-computed [{processorId, output}] entries")
			cmd.Flags().StringArrayVar(&secrets, "secret", nil, "key=value secret available to step actions (repeatable)")
			cmd.Flags().StringVar(&password, "password", "", "Password for a password-protected PDF (URL inputs only)")
			meta.attach(cmd)
			_ = cmd.MarkFlagRequired("workflow")
		},
		Subcommands: []*CommandDoc{newWorkflowBatchDoc(app)},
	}
}

type workflowParams struct {
	input       string
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

	ref, err := uploadOrResolveWith(ctx, app, cli, p.input, p.password)
	if err != nil {
		return err
	}

	in := client.CreateWorkflowRunInput{
		Workflow: &client.WorkflowRef{ID: p.workflowID, Version: p.version},
		File:     &ref,
		Metadata: p.metadata,
	}
	if p.priority > 0 {
		in.Priority = &p.priority
	}
	if p.outputsPath != "" {
		raw, err := readJSONFile(p.outputsPath, "--outputs")
		if err != nil {
			return err
		}
		var outputs []client.WorkflowProvidedOutput
		if err := json.Unmarshal(raw, &outputs); err != nil {
			return fmt.Errorf("--outputs: %w (expected JSON array of {processorId, output})", err)
		}
		in.Outputs = outputs
	}
	if len(p.secrets) > 0 {
		pairs, err := parseKVPairs("--secret", p.secrets)
		if err != nil {
			return err
		}
		secrets := make(map[string]any, len(pairs))
		for k, v := range pairs {
			secrets[k] = v
		}
		in.Secrets = secrets
	}

	run, err := cli.CreateWorkflowRun(ctx, in)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	if !p.wait {
		return renderWorkflowResult(app, run)
	}

	sp := app.IO.StartSpinner(fmt.Sprintf("Run %s: PENDING", run.ID))
	final, err := cli.WaitForWorkflowRun(ctx, run.ID, client.WaitProfileOptions(client.ProfileLong, p.timeout), func(r *client.WorkflowRun) {
		sp.Update(fmt.Sprintf("Run %s: %s", r.ID, r.Status))
	})
	sp.Stop("")
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	if err := renderWorkflowResult(app, final); err != nil {
		return err
	}
	switch final.Status {
	case client.StatusFailed:
		if final.FailureMessage != "" {
			return fmt.Errorf("run %s failed: %s", final.ID, final.FailureMessage)
		}
		return fmt.Errorf("run %s failed", final.ID)
	case client.StatusCancelled:
		return fmt.Errorf("run %s was cancelled", final.ID)
	case client.StatusRejected:
		return fmt.Errorf("run %s was rejected", final.ID)
	}
	return nil
}

func renderWorkflowResult(app *App, run *client.WorkflowRun) error {
	if app.Format != "" || app.JQ != "" || !app.IO.IsStdoutTTY() {
		return renderWithDefault(app, run, output.FormatJSON)
	}
	return renderWorkflowTTY(app, run)
}

func renderWorkflowTTY(app *App, run *client.WorkflowRun) error {
	pal := paletteFor(app.IO)
	fmt.Fprintf(app.IO.Out, "%s %s (%s, %d step%s)\n",
		statusIcon(pal, run.Status), run.ID, run.Status, len(run.StepRuns), pluralize(len(run.StepRuns)))
	if run.DashboardURL != "" {
		fmt.Fprintf(app.IO.Out, "  %s\n", pal.Dimf("Dashboard: %s", run.DashboardURL))
	}
	if run.Status == client.StatusNeedsReview {
		fmt.Fprintln(app.IO.Out, "  Awaiting human review at the dashboard URL above.")
	}
	if len(run.StepRuns) > 0 {
		fmt.Fprintln(app.IO.Out)
		rows := make([][]string, 0, len(run.StepRuns))
		for i, step := range run.StepRuns {
			name := ""
			stepType := ""
			if step.Step != nil {
				name = step.Step.Name
				stepType = step.Step.Type
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				name,
				stepType,
				string(step.Status),
			})
		}
		return output.RenderTable(app.IO.Out, []string{"step", "name", "type", "status"}, rows)
	}
	return nil
}

func statusIcon(p palette, s client.RunStatus) string {
	switch s {
	case client.StatusProcessed:
		return p.Green("✓")
	case client.StatusFailed, client.StatusRejected:
		return p.Red("✗")
	case client.StatusCancelled, client.StatusCancelling:
		return p.Dim("○")
	case client.StatusNeedsReview:
		return p.Yellow("⏸")
	case client.StatusPending, client.StatusProcessing:
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
