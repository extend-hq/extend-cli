package client

import (
	"context"
	"encoding/json"
)

type ClassifyRun struct {
	ID                string                    `json:"id"`
	Object            string                    `json:"object"`
	Status            RunStatus                 `json:"status"`
	Classifier        *ClassifierSummary        `json:"classifier,omitempty"`
	ClassifierVersion *ClassifierVersionSummary `json:"classifierVersion,omitempty"`
	File              *File                     `json:"file,omitempty"`
	ParseRunID        string                    `json:"parseRunId,omitempty"`
	Output            *ClassifyOutput           `json:"output,omitempty"`
	InitialOutput     *ClassifyOutput           `json:"initialOutput,omitempty"`
	ReviewedOutput    *ClassifyOutput           `json:"reviewedOutput,omitempty"`
	Reviewed          bool                      `json:"reviewed,omitempty"`
	Edited            bool                      `json:"edited,omitempty"`
	Config            json.RawMessage           `json:"config,omitempty"`
	Metadata          map[string]any            `json:"metadata,omitempty"`
	FailureReason     string                    `json:"failureReason,omitempty"`
	FailureMessage    string                    `json:"failureMessage,omitempty"`
	DashboardURL      string                    `json:"dashboardUrl,omitempty"`
	Usage             *Usage                    `json:"usage,omitempty"`
	CreatedAt         string                    `json:"createdAt,omitempty"`
	UpdatedAt         string                    `json:"updatedAt,omitempty"`
}

type ClassifierVersionSummary struct {
	ID           string `json:"id,omitempty"`
	Object       string `json:"object,omitempty"`
	Version      string `json:"version,omitempty"`
	Description  string `json:"description,omitempty"`
	ClassifierID string `json:"classifierId,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type ClassifyOutput struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Confidence float64           `json:"confidence"`
	Insights   []ClassifyInsight `json:"insights,omitempty"`
}

type ClassifyInsight struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type ClassifierSummary struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type ClassifierRef struct {
	ID             string          `json:"id"`
	Version        string          `json:"version,omitempty"`
	OverrideConfig json.RawMessage `json:"overrideConfig,omitempty"`
}

type CreateClassifyRunInput struct {
	Classifier *ClassifierRef  `json:"classifier,omitempty"`
	File       FileRef         `json:"file"`
	Priority   *int            `json:"priority,omitempty"`
	Metadata   map[string]any  `json:"metadata,omitempty"`
	Config     json.RawMessage `json:"config,omitempty"`
}

func (c *Client) CreateClassifyRun(ctx context.Context, in CreateClassifyRunInput) (*ClassifyRun, error) {
	var run ClassifyRun
	if err := c.postJSON(ctx, "/classify_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetClassifyRun(ctx context.Context, id string) (*ClassifyRun, error) {
	var run ClassifyRun
	if err := c.getJSON(ctx, "/classify_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) WaitForClassifyRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*ClassifyRun)) (*ClassifyRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*ClassifyRun, error) { return c.GetClassifyRun(ctx, id) },
		func(r *ClassifyRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}
