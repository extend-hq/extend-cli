package client

import (
	"context"
	"encoding/json"
)

type ParseRun struct {
	ID             string          `json:"id"`
	Object         string          `json:"object"`
	Status         RunStatus       `json:"status"`
	File           *File           `json:"file,omitempty"`
	Output         *ParseRunOutput `json:"output,omitempty"`
	OutputURL      string          `json:"outputUrl,omitempty"`
	Config         *ParseConfig    `json:"config,omitempty"`
	Metrics        *ParseMetrics   `json:"metrics,omitempty"`
	BatchID        string          `json:"batchId,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
	FailureReason  string          `json:"failureReason,omitempty"`
	FailureMessage string          `json:"failureMessage,omitempty"`
	CreatedAt      string          `json:"createdAt,omitempty"`
	UpdatedAt      string          `json:"updatedAt,omitempty"`
}

type ParseMetrics struct {
	ProcessingTimeMs int `json:"processingTimeMs,omitempty"`
	PageCount        int `json:"pageCount,omitempty"`
}

type ParseRunOutput struct {
	Chunks []ParseChunk `json:"chunks"`
	OCR    *OCROutput   `json:"ocr,omitempty"`
}

// OCROutput is the structured OCR data attached to a parse run when
// `config.advancedOptions.returnOcr.words = true` is set on the request.
type OCROutput struct {
	Words []OCRWord `json:"words"`
}

type OCRWord struct {
	Content     string      `json:"content"`
	BoundingBox BoundingBox `json:"boundingBox"`
	Confidence  float64     `json:"confidence"`
	PageNumber  int         `json:"pageNumber"`
}

// BoundingBox is a [0,1]-normalized rectangle anchored on the page. The
// server-side type carries the same field names; we mirror them verbatim so
// downstream code (e.g. Studio) can copy/paste between the two.
type BoundingBox struct {
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ParseChunk struct {
	ID       string         `json:"id"`
	Object   string         `json:"object"`
	Type     string         `json:"type"`
	Content  string         `json:"content"`
	Metadata *ChunkMetadata `json:"metadata,omitempty"`
	Blocks   []ParseBlock   `json:"blocks,omitempty"`
}

// ChunkMetadata is the per-chunk metadata returned by the server. The
// `sheet` field is only set for Excel inputs; otherwise pageRange is the only
// populated value.
type ChunkMetadata struct {
	PageRange *ChunkPageRange `json:"pageRange,omitempty"`
	Sheet     *ChunkSheet     `json:"sheet,omitempty"`
}

type ChunkPageRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type ChunkSheet struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	HeaderRows  [][]any `json:"headerRows,omitempty"`
	RowCount    int     `json:"rowCount"`
	ColumnCount int     `json:"columnCount"`
}

// ParseBlock is one structured block within a parse chunk. Block.Details is
// a discriminated union by Type (table_details, table_cell_details, figure_
// details, barcode_details, key_value_details, formula_details, or absent for
// plain text blocks); we keep it raw so callers can switch on Type and
// re-decode only the variant they care about.
type ParseBlock struct {
	ID            string          `json:"id"`
	Object        string          `json:"object"`
	ParentBlockID string          `json:"parentBlockId,omitempty"`
	Type          string          `json:"type"`
	Content       string          `json:"content"`
	Details       json.RawMessage `json:"details,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
	Polygon       []Point         `json:"polygon,omitempty"`
	BoundingBox   BoundingBox     `json:"boundingBox"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type ParseConfig struct {
	Target           string            `json:"target,omitempty"`
	Engine           string            `json:"engine,omitempty"`
	EngineVersion    string            `json:"engineVersion,omitempty"`
	ChunkingStrategy *ChunkingStrategy `json:"chunkingStrategy,omitempty"`
	// BlockOptions and AdvancedOptions are kept opaque (json.RawMessage) on
	// purpose. Both schemas are large (figures/tables/text/barcodes/keyValue/
	// formulas; pageRanges/excel options/parallelism/etc.) and nesting-rich;
	// passing them as raw JSON lets users keep server-side defaults while
	// overriding individual leaves without us hard-coding every leaf field.
	BlockOptions    json.RawMessage `json:"blockOptions,omitempty"`
	AdvancedOptions json.RawMessage `json:"advancedOptions,omitempty"`
}

type ChunkingStrategy struct {
	Type    string                   `json:"type,omitempty"`
	Options *ChunkingStrategyOptions `json:"options,omitempty"`
}

// ChunkingStrategyOptions tunes the size of generated chunks. Both bounds
// are character counts; nil means "server default".
type ChunkingStrategyOptions struct {
	MinCharacters *int `json:"minCharacters,omitempty"`
	MaxCharacters *int `json:"maxCharacters,omitempty"`
}

type CreateParseRunInput struct {
	File     FileRef        `json:"file"`
	Config   *ParseConfig   `json:"config,omitempty"`
	Priority *int           `json:"priority,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (c *Client) CreateParseRun(ctx context.Context, in CreateParseRunInput) (*ParseRun, error) {
	var run ParseRun
	if err := c.postJSON(ctx, "/parse_runs", in, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetParseRun(ctx context.Context, id string) (*ParseRun, error) {
	var run ParseRun
	if err := c.getJSON(ctx, "/parse_runs/"+id, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) WaitForParseRun(ctx context.Context, id string, opts WaitOptions, onPoll func(*ParseRun)) (*ParseRun, error) {
	return waitForRun(ctx,
		func(ctx context.Context) (*ParseRun, error) { return c.GetParseRun(ctx, id) },
		func(r *ParseRun) RunStatus { return r.Status },
		opts, onPoll,
	)
}
