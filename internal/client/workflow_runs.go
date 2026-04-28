package client

import (
	"context"
	"encoding/json"
)

type WorkflowRun struct {
	ID              string              `json:"id"`
	Object          string              `json:"object"`
	Status          RunStatus           `json:"status"`
	Workflow        *WorkflowSummary    `json:"workflow,omitempty"`
	WorkflowVersion *WorkflowVersionRef `json:"workflowVersion,omitempty"`
	DashboardURL    string              `json:"dashboardUrl,omitempty"`
	Files           []File              `json:"files,omitempty"`
	StepRuns        []WorkflowStepRun   `json:"stepRuns,omitempty"`
	BatchID         string              `json:"batchId,omitempty"`
	Reviewed        bool                `json:"reviewed,omitempty"`
	ReviewedByUser  string              `json:"reviewedByUser,omitempty"`
	ReviewedAt      string              `json:"reviewedAt,omitempty"`
	RejectionNote   string              `json:"rejectionNote,omitempty"`
	StartTime       string              `json:"startTime,omitempty"`
	EndTime         string              `json:"endTime,omitempty"`
	InitialRunAt    string              `json:"initialRunAt,omitempty"`
	FailureReason   string              `json:"failureReason,omitempty"`
	FailureMessage  string              `json:"failureMessage,omitempty"`
	Metadata        map[string]any      `json:"metadata,omitempty"`
	Usage           *Usage              `json:"usage,omitempty"`
	CreatedAt       string              `json:"createdAt,omitempty"`
	UpdatedAt       string              `json:"updatedAt,omitempty"`
}

type WorkflowSummary struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type WorkflowVersionRef struct {
	ID        string `json:"id,omitempty"`
	Object    string `json:"object,omitempty"`
	Version   string `json:"version,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// WorkflowStepRun is one element of WorkflowRun.StepRuns. Each step's Result
// is a discriminated union keyed by Step.Type — see the per-type Result
// helper methods below for typed access. The raw JSON is always preserved so
// callers can `--jq` into fields that haven't yet been promoted to typed Go
// fields, or fall back when they only care about a few keys.
type WorkflowStepRun struct {
	ID            string          `json:"id"`
	Object        string          `json:"object"`
	Status        RunStatus       `json:"status"`
	WorkflowRunID string          `json:"workflowRunId,omitempty"`
	Step          *WorkflowStep   `json:"step,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Files         []File          `json:"files,omitempty"`
}

type WorkflowStep struct {
	ID     string `json:"id"`
	Object string `json:"object,omitempty"`
	Name   string `json:"name,omitempty"`
	Type   string `json:"type,omitempty"`
}

// Step type values used as discriminators on WorkflowStep.Type. The set
// matches PublicStepType in the server's externalApi/versions/2026-02-09/
// types.ts.
const (
	StepTypeParse                  = "PARSE"
	StepTypeExtract                = "EXTRACT"
	StepTypeClassify               = "CLASSIFY"
	StepTypeSplit                  = "SPLIT"
	StepTypeMergeExtract           = "MERGE_EXTRACT"
	StepTypeConditionalExtract     = "CONDITIONAL_EXTRACT"
	StepTypeRuleValidation         = "RULE_VALIDATION"
	StepTypeExternalDataValidation = "EXTERNAL_DATA_VALIDATION"
)

// Step run statuses include WAITING and INVALIDATED, which RunStatus does
// not constrain. Provided here as documentation for callers comparing values.
const (
	StepStatusWaiting     RunStatus = "WAITING"
	StepStatusInvalidated RunStatus = "INVALIDATED"
)

// ParseStepResult is the typed payload of result on a parse step run.
type ParseStepResult struct {
	ParseRun *ParseRun `json:"parseRun,omitempty"`
}

// ExtractStepResult covers EXTRACT and CONDITIONAL_EXTRACT step runs (which
// share a result shape).
type ExtractStepResult struct {
	ExtractRun *ExtractRun `json:"extractRun,omitempty"`
}

type ClassifyStepResult struct {
	ClassifyRun *ClassifyRun `json:"classifyRun,omitempty"`
}

type SplitStepResult struct {
	SplitRun *SplitRun `json:"splitRun,omitempty"`
}

type MergeExtractStepResult struct {
	MergedExtractors []MergedExtractor `json:"mergedExtractors,omitempty"`
	ExtractRun       *ExtractRun       `json:"extractRun,omitempty"`
}

type MergedExtractor struct {
	ID        string `json:"id"`
	VersionID string `json:"versionId"`
	Name      string `json:"name"`
}

type RuleValidationStepResult struct {
	Rules     []ValidationRule `json:"rules"`
	AllPassed bool             `json:"allPassed"`
}

type ValidationRule struct {
	Name          string `json:"name"`
	Valid         bool   `json:"valid"`
	ValidArray    []bool `json:"validArray,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
	Error         string `json:"error,omitempty"`
}

type ExternalDataValidationStepResult struct {
	Response ExternalDataValidationResponse `json:"response"`
}

type ExternalDataValidationResponse struct {
	Status int            `json:"status"`
	Data   map[string]any `json:"data"`
}

// ParseResult / ExtractResult / ClassifyResult / SplitResult / MergeExtractResult /
// RuleValidationResult / ExternalDataValidationResult decode the step run's
// raw Result bytes into the typed shape for the matching Step.Type. Each
// returns ok=false (with no error) when the step type doesn't match, and a
// non-nil error only when the JSON is malformed for the requested shape.

func (s *WorkflowStepRun) ParseResult() (result *ParseStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeParse {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r ParseStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) ExtractResult() (result *ExtractStepResult, ok bool, err error) {
	if s.Step == nil || (s.Step.Type != StepTypeExtract && s.Step.Type != StepTypeConditionalExtract) {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r ExtractStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) ClassifyResult() (result *ClassifyStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeClassify {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r ClassifyStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) SplitResult() (result *SplitStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeSplit {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r SplitStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) MergeExtractResult() (result *MergeExtractStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeMergeExtract {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r MergeExtractStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) RuleValidationResult() (result *RuleValidationStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeRuleValidation {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r RuleValidationStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

func (s *WorkflowStepRun) ExternalDataValidationResult() (result *ExternalDataValidationStepResult, ok bool, err error) {
	if s.Step == nil || s.Step.Type != StepTypeExternalDataValidation {
		return nil, false, nil
	}
	if len(s.Result) == 0 || string(s.Result) == "null" {
		return nil, true, nil
	}
	var r ExternalDataValidationStepResult
	if err := json.Unmarshal(s.Result, &r); err != nil {
		return nil, true, err
	}
	return &r, true, nil
}

type WorkflowRef struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
}

// WorkflowProvidedOutput is one entry of CreateWorkflowRunInput.Outputs. The
// `output` field is a discriminated union (extract: {value}, classify:
// {id,type,confidence}, split: {splits[]}); we keep it as RawMessage so callers
// can pass any of the three shapes verbatim.
type WorkflowProvidedOutput struct {
	ProcessorID string          `json:"processorId"`
	Output      json.RawMessage `json:"output"`
}

// CreateWorkflowRunInput is POST /workflow_runs.
//
// The server schema accepts a SINGLE `file` only; there is no `files` array
// for single workflow runs (the batch endpoint takes per-input files instead).
type CreateWorkflowRunInput struct {
	Workflow *WorkflowRef             `json:"workflow,omitempty"`
	File     *FileRef                 `json:"file,omitempty"`
	Priority *int                     `json:"priority,omitempty"`
	Metadata map[string]any           `json:"metadata,omitempty"`
	Outputs  []WorkflowProvidedOutput `json:"outputs,omitempty"`
	Secrets  map[string]any           `json:"secrets,omitempty"`
}

func (c *Client) CreateWorkflowRun(ctx context.Context, in CreateWorkflowRunInput) (*WorkflowRun, error) {
	var run WorkflowRun
	if err := c.postJSON(ctx, "/workflow_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, id string) (*WorkflowRun, error) {
	var run WorkflowRun
	if err := c.getJSON(ctx, "/workflow_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) WaitForWorkflowRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*WorkflowRun)) (*WorkflowRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*WorkflowRun, error) { return c.GetWorkflowRun(ctx, id) },
		func(r *WorkflowRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}

func (c *Client) CancelWorkflowRun(ctx context.Context, id string) error {
	return c.cancelRun(ctx, "/workflow_runs/"+id+"/cancel")
}

func (c *Client) ListWorkflowRuns(ctx context.Context, opts ListRunsOptions) (*ListResponse[*WorkflowRun], error) {
	return listRuns[*WorkflowRun](ctx, c, KindWorkflow, "/workflow_runs", opts)
}

func (c *Client) UpdateWorkflowRun(ctx context.Context, id string, body json.RawMessage) (*WorkflowRun, error) {
	var run WorkflowRun
	if err := c.updateRaw(ctx, "/workflow_runs/"+id, body, &run); err != nil {
		return nil, err
	}
	return &run, nil
}
