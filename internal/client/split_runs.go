package client

import (
	"context"
	"encoding/json"
)

type SplitRun struct {
	ID              string                  `json:"id"`
	Object          string                  `json:"object"`
	Status          RunStatus               `json:"status"`
	Splitter        *SplitterSummary        `json:"splitter,omitempty"`
	SplitterVersion *SplitterVersionSummary `json:"splitterVersion,omitempty"`
	File            *File                   `json:"file,omitempty"`
	ParseRunID      string                  `json:"parseRunId,omitempty"`
	Output          *SplitOutput            `json:"output,omitempty"`
	InitialOutput   *SplitOutput            `json:"initialOutput,omitempty"`
	ReviewedOutput  *SplitOutput            `json:"reviewedOutput,omitempty"`
	Reviewed        bool                    `json:"reviewed,omitempty"`
	Edited          bool                    `json:"edited,omitempty"`
	Config          json.RawMessage         `json:"config,omitempty"`
	Metadata        map[string]any          `json:"metadata,omitempty"`
	FailureReason   string                  `json:"failureReason,omitempty"`
	FailureMessage  string                  `json:"failureMessage,omitempty"`
	DashboardURL    string                  `json:"dashboardUrl,omitempty"`
	Usage           *Usage                  `json:"usage,omitempty"`
	CreatedAt       string                  `json:"createdAt,omitempty"`
	UpdatedAt       string                  `json:"updatedAt,omitempty"`
}

type SplitterVersionSummary struct {
	ID          string `json:"id,omitempty"`
	Object      string `json:"object,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	SplitterID  string `json:"splitterId,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type SplitOutput struct {
	Splits []SplitSegment `json:"splits"`
}

type SplitSegment struct {
	ID               string `json:"id"`
	Type             string `json:"type,omitempty"`
	Name             string `json:"name,omitempty"`
	Identifier       string `json:"identifier,omitempty"`
	ClassificationID string `json:"classificationId,omitempty"`
	Observation      string `json:"observation,omitempty"`
	StartPage        int    `json:"startPage"`
	EndPage          int    `json:"endPage"`
	FileID           string `json:"fileId,omitempty"`
}

type SplitterSummary struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type SplitterRef struct {
	ID             string          `json:"id"`
	Version        string          `json:"version,omitempty"`
	OverrideConfig json.RawMessage `json:"overrideConfig,omitempty"`
}

type CreateSplitRunInput struct {
	Splitter *SplitterRef    `json:"splitter,omitempty"`
	File     FileRef         `json:"file"`
	Priority *int            `json:"priority,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
}

func (c *Client) CreateSplitRun(ctx context.Context, in CreateSplitRunInput) (*SplitRun, error) {
	var run SplitRun
	if err := c.postJSON(ctx, "/split_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetSplitRun(ctx context.Context, id string) (*SplitRun, error) {
	var run SplitRun
	if err := c.getJSON(ctx, "/split_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) WaitForSplitRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*SplitRun)) (*SplitRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*SplitRun, error) { return c.GetSplitRun(ctx, id) },
		func(r *SplitRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}
