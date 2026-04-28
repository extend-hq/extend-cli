package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// Extractor / Classifier / Splitter / Workflow are GET-by-id and list response
// shapes. The list endpoints return the lean Summary form (no draftVersion);
// the GET endpoints return the Detail form which embeds the full draft
// version. We use one struct per processor with `draftVersion` as omitempty
// so the same type round-trips both shapes — list responses simply have a
// nil DraftVersion.
type Extractor struct {
	ID           string            `json:"id"`
	Object       string            `json:"object"`
	Name         string            `json:"name"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
	DraftVersion *ProcessorVersion `json:"draftVersion,omitempty"`
}

type Classifier struct {
	ID           string            `json:"id"`
	Object       string            `json:"object"`
	Name         string            `json:"name"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
	DraftVersion *ProcessorVersion `json:"draftVersion,omitempty"`
}

type Splitter struct {
	ID           string            `json:"id"`
	Object       string            `json:"object"`
	Name         string            `json:"name"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
	DraftVersion *ProcessorVersion `json:"draftVersion,omitempty"`
}

type Workflow struct {
	ID           string            `json:"id"`
	Object       string            `json:"object"`
	Name         string            `json:"name"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
	DraftVersion *ProcessorVersion `json:"draftVersion,omitempty"`
}

// ProcessorVersion unifies the four processor version shapes into one struct
// because they share most fields but differ in the parent-ID key
// (extractorId/classifierId/splitterId/workflowId) and content payload
// (extract/classify/split versions carry `config`; workflow versions carry
// `name` and `steps` instead of `description` and `config`). Only the fields
// relevant to the actual processor type are populated; the rest are omitempty.
type ProcessorVersion struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`

	ExtractorID  string `json:"extractorId,omitempty"`
	ClassifierID string `json:"classifierId,omitempty"`
	SplitterID   string `json:"splitterId,omitempty"`
	WorkflowID   string `json:"workflowId,omitempty"`

	// Config is populated for extractor/classifier/splitter versions only.
	// Steps is populated for workflow versions only. Both are kept opaque
	// because their schemas are large and frequently extended.
	Config json.RawMessage `json:"config,omitempty"`
	Steps  json.RawMessage `json:"steps,omitempty"`
}

type ListProcessorsOptions struct {
	Limit     int
	PageToken string
	SortBy    string
	SortDir   string
}

func (o ListProcessorsOptions) query() string {
	return ListRunsOptions{
		Limit:     o.Limit,
		PageToken: o.PageToken,
		SortBy:    o.SortBy,
		SortDir:   o.SortDir,
	}.query()
}

func (c *Client) ListExtractors(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*Extractor], error) {
	var out ListResponse[*Extractor]
	if err := c.getJSON(ctx, "/extractors"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetExtractor(ctx context.Context, id string) (*Extractor, error) {
	var p Extractor
	if err := c.getJSON(ctx, "/extractors/"+id, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) ListExtractorVersions(ctx context.Context, id string) (*ListResponse[*ProcessorVersion], error) {
	var out ListResponse[*ProcessorVersion]
	if err := c.getJSON(ctx, "/extractors/"+id+"/versions", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetExtractorVersion(ctx context.Context, id, version string) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.getJSON(ctx, "/extractors/"+id+"/versions/"+version, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) ListClassifiers(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*Classifier], error) {
	var out ListResponse[*Classifier]
	if err := c.getJSON(ctx, "/classifiers"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetClassifier(ctx context.Context, id string) (*Classifier, error) {
	var p Classifier
	if err := c.getJSON(ctx, "/classifiers/"+id, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) ListClassifierVersions(ctx context.Context, id string) (*ListResponse[*ProcessorVersion], error) {
	var out ListResponse[*ProcessorVersion]
	if err := c.getJSON(ctx, "/classifiers/"+id+"/versions", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetClassifierVersion(ctx context.Context, id, version string) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.getJSON(ctx, "/classifiers/"+id+"/versions/"+version, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) ListSplitters(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*Splitter], error) {
	var out ListResponse[*Splitter]
	if err := c.getJSON(ctx, "/splitters"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSplitter(ctx context.Context, id string) (*Splitter, error) {
	var p Splitter
	if err := c.getJSON(ctx, "/splitters/"+id, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) ListSplitterVersions(ctx context.Context, id string) (*ListResponse[*ProcessorVersion], error) {
	var out ListResponse[*ProcessorVersion]
	if err := c.getJSON(ctx, "/splitters/"+id+"/versions", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSplitterVersion(ctx context.Context, id, version string) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.getJSON(ctx, "/splitters/"+id+"/versions/"+version, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) ListWorkflows(ctx context.Context, opts ListProcessorsOptions) (*ListResponse[*Workflow], error) {
	var out ListResponse[*Workflow]
	if err := c.getJSON(ctx, "/workflows"+opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	var p Workflow
	if err := c.getJSON(ctx, "/workflows/"+id, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) ListWorkflowVersions(ctx context.Context, id string) (*ListResponse[*ProcessorVersion], error) {
	var out ListResponse[*ProcessorVersion]
	if err := c.getJSON(ctx, "/workflows/"+id+"/versions", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWorkflowVersion(ctx context.Context, id, version string) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.getJSON(ctx, "/workflows/"+id+"/versions/"+version, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) postRaw(ctx context.Context, path string, body json.RawMessage, out any) error {
	resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(body), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) updateRaw(ctx context.Context, path string, body json.RawMessage, out any) error {
	resp, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(body), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if resp.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) CreateExtractor(ctx context.Context, body json.RawMessage) (*Extractor, error) {
	var p Extractor
	if err := c.postRaw(ctx, "/extractors", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) UpdateExtractor(ctx context.Context, id string, body json.RawMessage) (*Extractor, error) {
	var p Extractor
	if err := c.updateRaw(ctx, "/extractors/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) CreateExtractorVersion(ctx context.Context, id string, body json.RawMessage) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.postRaw(ctx, "/extractors/"+id+"/versions", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) CreateClassifier(ctx context.Context, body json.RawMessage) (*Classifier, error) {
	var p Classifier
	if err := c.postRaw(ctx, "/classifiers", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) UpdateClassifier(ctx context.Context, id string, body json.RawMessage) (*Classifier, error) {
	var p Classifier
	if err := c.updateRaw(ctx, "/classifiers/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) CreateClassifierVersion(ctx context.Context, id string, body json.RawMessage) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.postRaw(ctx, "/classifiers/"+id+"/versions", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) CreateSplitter(ctx context.Context, body json.RawMessage) (*Splitter, error) {
	var p Splitter
	if err := c.postRaw(ctx, "/splitters", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) UpdateSplitter(ctx context.Context, id string, body json.RawMessage) (*Splitter, error) {
	var p Splitter
	if err := c.updateRaw(ctx, "/splitters/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) CreateSplitterVersion(ctx context.Context, id string, body json.RawMessage) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.postRaw(ctx, "/splitters/"+id+"/versions", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) CreateWorkflow(ctx context.Context, body json.RawMessage) (*Workflow, error) {
	var p Workflow
	if err := c.postRaw(ctx, "/workflows", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Client) CreateWorkflowVersion(ctx context.Context, id string, body json.RawMessage) (*ProcessorVersion, error) {
	var v ProcessorVersion
	if err := c.postRaw(ctx, "/workflows/"+id+"/versions", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
