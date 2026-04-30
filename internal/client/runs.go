package client

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type RunStatus string

const (
	StatusPending     RunStatus = "PENDING"
	StatusProcessing  RunStatus = "PROCESSING"
	StatusProcessed   RunStatus = "PROCESSED"
	StatusFailed      RunStatus = "FAILED"
	StatusCancelled   RunStatus = "CANCELLED"
	StatusNeedsReview RunStatus = "NEEDS_REVIEW"
	StatusRejected    RunStatus = "REJECTED"
	StatusCancelling  RunStatus = "CANCELLING"
)

// TerminalSuccessStates lists the run statuses that represent a successful
// terminal outcome. Action commands (extract, classify, ...) exit zero on
// these.
var TerminalSuccessStates = []RunStatus{StatusProcessed}

// TerminalFailureStates lists the run statuses that represent a failed
// terminal outcome and cause action commands to exit non-zero. NEEDS_REVIEW
// is intentionally not in this list: it pauses for human action but the run
// itself has not failed. Per-kind subsets apply (parse runs cannot be
// CANCELLED or REJECTED), but commands check membership here.
var TerminalFailureStates = []RunStatus{StatusFailed, StatusCancelled, StatusRejected}

// TerminalReviewStates lists statuses that are terminal but indicate the
// run is awaiting a human decision rather than complete or failed.
var TerminalReviewStates = []RunStatus{StatusNeedsReview}

// IsTerminalFailure reports whether s is a terminal-failure state. Use this
// in exit-code logic instead of comparing against statuses individually.
func (s RunStatus) IsTerminalFailure() bool {
	for _, t := range TerminalFailureStates {
		if s == t {
			return true
		}
	}
	return false
}

func (s RunStatus) IsTerminal() bool {
	switch s {
	case StatusProcessed, StatusFailed, StatusCancelled, StatusNeedsReview, StatusRejected:
		return true
	}
	return false
}

type RunKind string

const (
	KindExtract  RunKind = "extract"
	KindParse    RunKind = "parse"
	KindClassify RunKind = "classify"
	KindSplit    RunKind = "split"
	KindWorkflow RunKind = "workflow"
	KindEdit     RunKind = "edit"
)

func RunKindFromID(id string) (RunKind, bool) {
	switch {
	case strings.HasPrefix(id, "exr_"):
		return KindExtract, true
	case strings.HasPrefix(id, "pr_"):
		return KindParse, true
	case strings.HasPrefix(id, "clr_"):
		return KindClassify, true
	case strings.HasPrefix(id, "splr_"):
		return KindSplit, true
	case strings.HasPrefix(id, "workflow_run_"):
		return KindWorkflow, true
	case strings.HasPrefix(id, "edr_"):
		return KindEdit, true
	}
	return "", false
}

type ExtractRun struct {
	ID               string                   `json:"id"`
	Object           string                   `json:"object"`
	Status           RunStatus                `json:"status"`
	Extractor        *ExtractorSummary        `json:"extractor,omitempty"`
	ExtractorVersion *ExtractorVersionSummary `json:"extractorVersion,omitempty"`
	File             *File                    `json:"file,omitempty"`
	ParseRunID       string                   `json:"parseRunId,omitempty"`
	Output           *ExtractOutput           `json:"output,omitempty"`
	InitialOutput    *ExtractOutput           `json:"initialOutput,omitempty"`
	ReviewedOutput   *ExtractOutput           `json:"reviewedOutput,omitempty"`
	Reviewed         bool                     `json:"reviewed,omitempty"`
	Edited           bool                     `json:"edited,omitempty"`
	Edits            json.RawMessage          `json:"edits,omitempty"`
	Config           json.RawMessage          `json:"config,omitempty"`
	Metadata         map[string]any           `json:"metadata,omitempty"`
	FailureReason    string                   `json:"failureReason,omitempty"`
	FailureMessage   string                   `json:"failureMessage,omitempty"`
	DashboardURL     string                   `json:"dashboardUrl,omitempty"`
	Usage            *Usage                   `json:"usage,omitempty"`
	CreatedAt        string                   `json:"createdAt,omitempty"`
	UpdatedAt        string                   `json:"updatedAt,omitempty"`
}

// ExtractOutput is the verbatim server JSON for an extract result. The
// server returns one of two shapes depending on the extractor: the modern
// {value, metadata} envelope or a legacy flat field-name->result map.
// Callers that need typed access can json.Unmarshal it into whichever shape
// they expect.
type ExtractOutput = json.RawMessage

type ExtractorSummary struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ExtractorVersionSummary is the embedded version info on an ExtractRun.
// The parent processor ID is keyed `extractorId` (mirrored on Classifier as
// `classifierId` and Splitter as `splitterId`); these three summaries differ
// only in that key.
type ExtractorVersionSummary struct {
	ID          string `json:"id,omitempty"`
	Object      string `json:"object,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	ExtractorID string `json:"extractorId,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type Usage struct {
	Credits float64 `json:"credits"`
}

type ExtractorRef struct {
	ID             string          `json:"id"`
	Version        string          `json:"version,omitempty"`
	OverrideConfig json.RawMessage `json:"overrideConfig,omitempty"`
}

type CreateExtractRunInput struct {
	Extractor *ExtractorRef   `json:"extractor,omitempty"`
	Config    json.RawMessage `json:"config,omitempty"`
	File      FileRef         `json:"file"`
	Priority  *int            `json:"priority,omitempty"`
	Metadata  map[string]any  `json:"metadata,omitempty"`
}

func (c *Client) CreateExtractRun(ctx context.Context, in CreateExtractRunInput) (*ExtractRun, error) {
	var run ExtractRun
	if err := c.postJSON(ctx, "/extract_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetExtractRun(ctx context.Context, id string) (*ExtractRun, error) {
	var run ExtractRun
	if err := c.getJSON(ctx, "/extract_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

type WaitOptions struct {
	Interval    time.Duration
	MaxInterval time.Duration
	Timeout     time.Duration
}

func waitForRun[T any](
	ctx context.Context,
	get func(context.Context) (T, error),
	statusOf func(T) RunStatus,
	opts WaitOptions,
	onPoll func(T),
) (T, error) {
	opts = applyWaitDefaults(opts)
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	var zero T
	delay := opts.Interval
	for {
		run, err := get(ctx)
		if err != nil {
			return zero, err
		}
		if onPoll != nil {
			onPoll(run)
		}
		if statusOf(run).IsTerminal() {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
		if delay < opts.MaxInterval {
			delay = min(delay*5/4, opts.MaxInterval)
		}
	}
}

func applyWaitDefaults(opts WaitOptions) WaitOptions {
	short := waitProfileTable[ProfileShort]
	if opts.Interval <= 0 {
		opts.Interval = short.Interval
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = short.MaxInterval
	}
	return opts
}

func (c *Client) WaitForExtractRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*ExtractRun)) (*ExtractRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*ExtractRun, error) { return c.GetExtractRun(ctx, id) },
		func(r *ExtractRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}

func decodeJSON(body []byte, v any) error {
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}
