package main

// Go-struct fixtures backing the stub's canned responses. These are the
// only IDs that may appear in agent-emitted commands without being
// flagged as fabricated. Adding a new fixture here = giving the agent
// a new legitimate ID to discover via list/get.
//
// Keep the fixture set small. The fabrication checker is more
// expressive when the legitimate ID set is sparse.

type Extractor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Classifier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Splitter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Workflow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType,omitempty"`
}

// fixtureExtractors is the canonical list returned by `extractors list`
// in real_responses mode. Two entries is enough to exercise "agent
// picked one" without making the right pick obvious.
var fixtureExtractors = []Extractor{
	{ID: "ex_invoiceQ3", Name: "Q3 invoices", Description: "Line items, totals, and remit-to address from invoice PDFs."},
	{ID: "ex_receipt01", Name: "Receipt totals", Description: "Total, tax, and merchant from receipt images."},
}

var fixtureClassifiers = []Classifier{
	{ID: "cl_contracts01", Name: "Contract type", Description: "Classify contracts as MSA, SOW, NDA, or Other."},
}

var fixtureSplitters = []Splitter{
	{ID: "spl_statements01", Name: "Customer statements", Description: "Split combined statement PDFs into per-customer segments."},
}

var fixtureWorkflows = []Workflow{
	{ID: "workflow_invoice", Name: "Invoice processing", Description: "Extract → validate → notify."},
}

var fixtureFiles = []File{
	{ID: "file_inv001", Name: "invoice.pdf", MIMEType: "application/pdf"},
	{ID: "file_inv002", Name: "invoice2.pdf", MIMEType: "application/pdf"},
}

// Run represents a terminal-state run record (any type), shaped close
// to the real CLI's run rendering.
type Run struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	ProcessedAt string         `json:"processedAt"`
	Output      map[string]any `json:"output,omitempty"`
	Failure     *RunFailure    `json:"failure,omitempty"`
}

type RunFailure struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// fixtureRuns covers a few canned terminal-state runs callable by ID.
var fixtureRuns = map[string]Run{
	"exr_demo_processed": {
		ID:          "exr_demo_processed",
		Type:        "extract",
		Status:      "PROCESSED",
		ProcessedAt: "2026-04-30T12:34:56Z",
		Output: map[string]any{
			"value": map[string]any{
				"invoice_id": "INV-1024",
				"line_items": []map[string]any{
					{"description": "Widget", "quantity": 2, "amount": 29.99},
					{"description": "Sprocket", "quantity": 1, "amount": 14.5},
				},
				"total": 74.48,
			},
		},
	},
	"exr_demo_failed": {
		ID:          "exr_demo_failed",
		Type:        "extract",
		Status:      "FAILED",
		ProcessedAt: "2026-04-30T12:35:00Z",
		Failure: &RunFailure{
			Reason:  "EXTRACTOR_ERROR",
			Message: "Document language unsupported",
		},
	},
	"pr_demo_processed": {
		ID:          "pr_demo_processed",
		Type:        "parse",
		Status:      "PROCESSED",
		ProcessedAt: "2026-04-30T12:36:00Z",
		Output: map[string]any{
			"chunks": []map[string]any{
				{"type": "text", "content": "# Contract\n\nThis agreement..."},
			},
		},
	},
}

// fixtureFailedRuns is the list of FAILED extract runs used by
// `runs list --status FAILED`. Larger than the get-by-id map so
// pagination has meaningful pages to split.
var fixtureFailedRuns = []Run{
	{ID: "exr_demo_failed", Type: "extract", Status: "FAILED", ProcessedAt: "2026-04-30T12:35:00Z",
		Failure: &RunFailure{Reason: "EXTRACTOR_ERROR", Message: "Document language unsupported"}},
	{ID: "exr_failed_002", Type: "extract", Status: "FAILED", ProcessedAt: "2026-04-29T08:14:32Z",
		Failure: &RunFailure{Reason: "TIMEOUT", Message: "Run exceeded 30m timeout"}},
	{ID: "exr_failed_003", Type: "extract", Status: "FAILED", ProcessedAt: "2026-04-28T19:45:11Z",
		Failure: &RunFailure{Reason: "EXTRACTOR_ERROR", Message: "Schema field 'total' not found"}},
	{ID: "exr_failed_004", Type: "extract", Status: "FAILED", ProcessedAt: "2026-04-27T14:02:50Z",
		Failure: &RunFailure{Reason: "INTERNAL_ERROR", Message: "Transient error"}},
	{ID: "exr_failed_005", Type: "extract", Status: "FAILED", ProcessedAt: "2026-04-26T09:30:00Z",
		Failure: &RunFailure{Reason: "EXTRACTOR_ERROR", Message: "Page count exceeds limit"}},
}

// fixtureEditRuns backs `edit runs list`.
var fixtureEditRuns = []Run{
	{ID: "edr_demo_w9", Type: "edit", Status: "PROCESSED", ProcessedAt: "2026-04-30T10:12:00Z"},
	{ID: "edr_demo_nda", Type: "edit", Status: "FAILED", ProcessedAt: "2026-04-29T16:40:00Z",
		Failure: &RunFailure{Reason: "NO_FIELDS_DETECTED", Message: "No fillable fields were found in the document"}},
}

// fixtureEditTemplates backs `edit templates list`; agents may pass these
// IDs to `edit --template` without fabricating.
var fixtureEditTemplates = []map[string]any{
	{"object": "edit_template", "id": "edt_demo_w9", "name": "W-9 (2024)",
		"file":      map[string]any{"object": "file", "id": "file_demo_fw9", "name": "fw9.pdf"},
		"createdAt": "2026-04-01T09:00:00Z", "updatedAt": "2026-04-20T09:00:00Z"},
	{"object": "edit_template", "id": "edt_demo_nda", "name": "Acme mutual NDA",
		"file":      map[string]any{"object": "file", "id": "file_demo_nda", "name": "nda.pdf"},
		"createdAt": "2026-03-11T09:00:00Z", "updatedAt": "2026-03-15T09:00:00Z"},
}

// fixtureEditTemplateConfigs are the saved fill defaults `edit templates
// get` adds to the fixture templates; the list endpoint omits them. Each
// has a schema, as a template saved from the dashboard does.
var fixtureEditTemplateConfigs = map[string]map[string]any{
	"edt_demo_w9": {
		"instructions": "Fill the form using the provided values.",
		"schema":       editSchema("name", "tin", "address"),
	},
	"edt_demo_nda": {
		"instructions": "Fill in the counterparty and the effective date.",
		"schema":       editSchema("counterparty_name", "effective_date"),
	},
}

func editSchema(fields ...string) map[string]any {
	props := map[string]any{}
	for _, f := range fields {
		props[f] = map[string]any{"type": "string", "extend_edit:value": nil}
	}
	return map[string]any{"type": "object", "properties": props}
}
