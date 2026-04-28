package client

import (
	"context"
	"encoding/json"
)

// EditRun is the response shape for /edit_runs (POST and GET).
//
// Status is one of PROCESSING|PROCESSED|FAILED — narrower than RunStatus,
// which is shared with the other run kinds.
type EditRun struct {
	ID             string         `json:"id"`
	Object         string         `json:"object"`
	Status         RunStatus      `json:"status"`
	File           *File          `json:"file,omitempty"`
	Config         *EditRunConfig `json:"config,omitempty"`
	Output         *EditOutput    `json:"output,omitempty"`
	Metrics        *EditMetrics   `json:"metrics,omitempty"`
	Usage          *Usage         `json:"usage,omitempty"`
	FailureReason  string         `json:"failureReason,omitempty"`
	FailureMessage string         `json:"failureMessage,omitempty"`
}

// EditMetrics mirrors the server's EditRunMetrics shape and is non-null only
// when status == PROCESSED. All durations are integer milliseconds.
type EditMetrics struct {
	ProcessingTimeMs      int `json:"processingTimeMs"`
	PageCount             int `json:"pageCount"`
	FieldCount            int `json:"fieldCount"`
	FieldsDetectedCount   int `json:"fieldsDetectedCount"`
	FieldsAnnotatedCount  int `json:"fieldsAnnotatedCount"`
	FieldDetectionTimeMs  int `json:"fieldDetectionTimeMs"`
	FieldAnnotationTimeMs int `json:"fieldAnnotationTimeMs"`
	FieldFillingTimeMs    int `json:"fieldFillingTimeMs"`
}

type EditOutput struct {
	EditedFile   *EditedFile    `json:"editedFile,omitempty"`
	FilledValues map[string]any `json:"filledValues,omitempty"`
}

type EditedFile struct {
	ID           string `json:"id"`
	PresignedURL string `json:"presignedUrl,omitempty"`
}

// EditAdvancedOptions controls per-field detection and PDF rendering for edit runs.
// FlattenPdf is honored only by edit runs (POST /edit_runs); schema generation
// (POST /edit_schemas/generate) reads the same struct but ignores FlattenPdf.
type EditAdvancedOptions struct {
	NativeFieldsOnly    *bool `json:"nativeFieldsOnly,omitempty"`
	TableParsingEnabled *bool `json:"tableParsingEnabled,omitempty"`
	FlattenPdf          *bool `json:"flattenPdf,omitempty"`
	RadioEnumsEnabled   *bool `json:"radioEnumsEnabled,omitempty"`
}

// EditRunConfig is the inner config for POST /edit_runs and the .config field
// of an edit-run response. The CLI never POSTs sync `/edit`, so this is the
// only edit-run config shape.
type EditRunConfig struct {
	Schema                       json.RawMessage      `json:"schema,omitempty"`
	Instructions                 string               `json:"instructions,omitempty"`
	SchemaGenerationInstructions string               `json:"schemaGenerationInstructions,omitempty"`
	AdvancedOptions              *EditAdvancedOptions `json:"advancedOptions,omitempty"`
}

// EditSchemaGenerationConfig is the inner config for POST /edit_schemas/generate.
// It carries `inputSchema` (a starting-point JSON Schema), not `schema` like
// EditRunConfig — the names differ on the wire and must not be conflated.
type EditSchemaGenerationConfig struct {
	InputSchema     json.RawMessage      `json:"inputSchema,omitempty"`
	Instructions    string               `json:"instructions,omitempty"`
	AdvancedOptions *EditAdvancedOptions `json:"advancedOptions,omitempty"`
}

type GenerateEditSchemaInput struct {
	File   FileRef                     `json:"file"`
	Config *EditSchemaGenerationConfig `json:"config,omitempty"`
}

type CreateEditRunInput struct {
	File   FileRef        `json:"file"`
	Config *EditRunConfig `json:"config,omitempty"`
}

func (c *Client) GenerateEditSchema(ctx context.Context, in GenerateEditSchemaInput) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.postJSON(ctx, "/edit_schemas/generate", in, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) CreateEditRun(ctx context.Context, in CreateEditRunInput) (*EditRun, error) {
	var run EditRun
	if err := c.postJSON(ctx, "/edit_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetEditRun(ctx context.Context, id string) (*EditRun, error) {
	var run EditRun
	if err := c.getJSON(ctx, "/edit_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) WaitForEditRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*EditRun)) (*EditRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*EditRun, error) { return c.GetEditRun(ctx, id) },
		func(r *EditRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}

// Note: there is no LIST /edit_runs endpoint on the server (`routes.ts` only
// mounts POST/GET-by-id/DELETE). `extend runs list --type edit` therefore
// returns an error rather than 404-ing the user. Re-add a `ListEditRuns`
// method if the server ever exposes the list route.
