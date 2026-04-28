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

// ExtractOutput is the 2026-02-09 extract result shape. The struct also
// carries the original bytes in Raw so legacy (pre-2026-02-09) outputs that
// don't fit the {value, metadata} shape can be round-tripped through the
// CLI's `--json` output unchanged. Callers reading typed fields should
// check `Value != nil` to distinguish legacy from new shapes.
type ExtractOutput struct {
	Value    map[string]any                  `json:"value,omitempty"`
	Metadata map[string]ExtractFieldMetadata `json:"metadata,omitempty"`

	// Raw is the verbatim server JSON, set by UnmarshalJSON. Marshalling an
	// ExtractOutput that has Raw set returns the original bytes (preserving
	// any legacy fields the typed Value/Metadata can't represent); callers
	// who modify Value/Metadata in place must clear Raw to opt back into
	// re-encoding.
	Raw json.RawMessage `json:"-"`
}

// ExtractFieldMetadata is one entry of ExtractOutput.Metadata, keyed by the
// extractor field name. Confidence values are in [0,1]; Citations and
// Insights may be omitted by the server when irrelevant for the field type.
type ExtractFieldMetadata struct {
	OCRConfidence      *float64          `json:"ocrConfidence,omitempty"`
	LogprobsConfidence *float64          `json:"logprobsConfidence,omitempty"`
	ReviewAgentScore   *float64          `json:"reviewAgentScore,omitempty"`
	Citations          []ExtractCitation `json:"citations,omitempty"`
	Insights           []ExtractInsight  `json:"insights,omitempty"`
}

type ExtractCitation struct {
	Page          *ExtractCitationPage `json:"page,omitempty"`
	ReferenceText string               `json:"referenceText,omitempty"`
	Polygon       []Point              `json:"polygon,omitempty"`
}

type ExtractCitationPage struct {
	Number int     `json:"number,omitempty"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
}

// ExtractInsight is one of the per-field annotations the server emits to
// explain its choice. Type is a closed enum on the server.
type ExtractInsight struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (o *ExtractOutput) UnmarshalJSON(data []byte) error {
	o.Raw = make(json.RawMessage, len(data))
	copy(o.Raw, data)
	type plain struct {
		Value    map[string]any                  `json:"value,omitempty"`
		Metadata map[string]ExtractFieldMetadata `json:"metadata,omitempty"`
	}
	// Best-effort decode. Legacy outputs (a flat field-name->result map) won't
	// match this shape; we leave Value/Metadata empty in that case and rely
	// on Raw for round-tripping.
	var p plain
	if err := json.Unmarshal(data, &p); err == nil {
		o.Value = p.Value
		o.Metadata = p.Metadata
	}
	return nil
}

func (o ExtractOutput) MarshalJSON() ([]byte, error) {
	if len(o.Raw) > 0 {
		return o.Raw, nil
	}
	type plain struct {
		Value    map[string]any                  `json:"value,omitempty"`
		Metadata map[string]ExtractFieldMetadata `json:"metadata,omitempty"`
	}
	return json.Marshal(plain{Value: o.Value, Metadata: o.Metadata})
}

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
	if opts.Interval <= 0 {
		opts.Interval = 1 * time.Second
	}
	if opts.MaxInterval <= 0 {
		opts.MaxInterval = 10 * time.Second
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
