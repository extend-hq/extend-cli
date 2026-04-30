package client

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// requireJSONEqual decodes both sides and compares them deeply, so insignificant
// whitespace and key ordering don't fail the test.
func requireJSONEqual(t *testing.T, got, want []byte, ctx string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: bad got json: %v\n%s", ctx, err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("%s: bad want json: %v\n%s", ctx, err, want)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if !bytes.Equal(gb, wb) {
		t.Errorf("%s: json mismatch\n got:  %s\n want: %s", ctx, gb, wb)
	}
}

func TestExtractorRefMarshalsOverrideConfig(t *testing.T) {
	ref := ExtractorRef{
		ID:             "ex_abc",
		Version:        "3",
		OverrideConfig: json.RawMessage(`{"fields":[{"key":"foo"}]}`),
	}
	got, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := []byte(`{"id":"ex_abc","version":"3","overrideConfig":{"fields":[{"key":"foo"}]}}`)
	requireJSONEqual(t, got, want, "ExtractorRef with overrideConfig")
}

func TestExtractorRefOmitsOverrideConfigWhenEmpty(t *testing.T) {
	ref := ExtractorRef{ID: "ex_abc"}
	got, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(got), "overrideConfig") {
		t.Errorf("expected no overrideConfig key when unset, got %s", got)
	}
}

func TestClassifierRefMarshalsOverrideConfig(t *testing.T) {
	ref := ClassifierRef{ID: "cl_x", OverrideConfig: json.RawMessage(`{"k":1}`)}
	got, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"overrideConfig":{"k":1}`) {
		t.Errorf("expected overrideConfig in output, got %s", got)
	}
}

func TestSplitterRefMarshalsOverrideConfig(t *testing.T) {
	ref := SplitterRef{ID: "sp_x", OverrideConfig: json.RawMessage(`{"k":1}`)}
	got, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"overrideConfig":{"k":1}`) {
		t.Errorf("expected overrideConfig in output, got %s", got)
	}
}

func TestCreateExtractRunInputAcceptsInlineConfig(t *testing.T) {
	in := CreateExtractRunInput{
		Config: json.RawMessage(`{"fields":[{"key":"foo","type":"string"}]}`),
		File:   FileRef{ID: "file_abc"},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(got), `"config":{"fields":[`) {
		t.Errorf("expected inline config, got %s", got)
	}
	if strings.Contains(string(got), `"extractor"`) {
		t.Errorf("extractor should be omitted when only config is set, got %s", got)
	}
}

func TestCreateExtractRunInputWithExtractorRef(t *testing.T) {
	in := CreateExtractRunInput{
		Extractor: &ExtractorRef{ID: "ex_abc", Version: "1"},
		File:      FileRef{ID: "file_abc"},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := []byte(`{"extractor":{"id":"ex_abc","version":"1"},"file":{"id":"file_abc"}}`)
	requireJSONEqual(t, got, want, "CreateExtractRunInput with extractor")
}

func TestExtractorVersionSummaryUnmarshalKeepsExtractorID(t *testing.T) {
	body := []byte(`{
		"object": "extractor_version",
		"id": "exv_abc",
		"version": "3",
		"description": "v3 changes",
		"extractorId": "ex_root",
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	var v ExtractorVersionSummary
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.ExtractorID != "ex_root" {
		t.Errorf("ExtractorID = %q, want ex_root", v.ExtractorID)
	}
	if v.Description != "v3 changes" {
		t.Errorf("Description = %q, want v3 changes", v.Description)
	}
	if v.Object != "extractor_version" {
		t.Errorf("Object = %q, want extractor_version", v.Object)
	}
}

func TestClassifierVersionSummaryUnmarshalKeepsClassifierID(t *testing.T) {
	body := []byte(`{"object":"classifier_version","id":"clv_x","version":"1","classifierId":"cl_root","description":null}`)
	var v ClassifierVersionSummary
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.ClassifierID != "cl_root" {
		t.Errorf("ClassifierID = %q, want cl_root", v.ClassifierID)
	}
}

func TestSplitterVersionSummaryUnmarshalKeepsSplitterID(t *testing.T) {
	body := []byte(`{"object":"splitter_version","id":"splv_x","version":"2","splitterId":"sp_root"}`)
	var v SplitterVersionSummary
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.SplitterID != "sp_root" {
		t.Errorf("SplitterID = %q, want sp_root", v.SplitterID)
	}
}

func TestExtractRunDecodesExtractorVersionSummary(t *testing.T) {
	body := []byte(`{
		"id":"exr_abc",
		"object":"extract_run",
		"status":"PROCESSED",
		"extractorVersion":{"id":"exv_v3","object":"extractor_version","version":"3","description":"v3","extractorId":"ex_root","createdAt":"2026-01-01T00:00:00Z"}
	}`)
	var r ExtractRun
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ExtractorVersion == nil {
		t.Fatal("ExtractorVersion is nil")
	}
	if r.ExtractorVersion.ExtractorID != "ex_root" {
		t.Errorf("ExtractorID = %q, want ex_root", r.ExtractorVersion.ExtractorID)
	}
	if r.ExtractorVersion.Description != "v3" {
		t.Errorf("Description = %q, want v3", r.ExtractorVersion.Description)
	}
}

func TestWorkflowVersionRefDecodesObjectAndCreatedAt(t *testing.T) {
	body := []byte(`{"object":"workflow_version","id":"wfv_x","version":"5","name":"prod","createdAt":"2026-02-09T00:00:00Z"}`)
	var v WorkflowVersionRef
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Object != "workflow_version" {
		t.Errorf("Object = %q, want workflow_version", v.Object)
	}
	if v.CreatedAt == "" {
		t.Error("CreatedAt should be populated")
	}
}

func TestCreateEditRunInputNestsConfig(t *testing.T) {
	tru := true
	in := CreateEditRunInput{
		File: FileRef{ID: "file_x"},
		Config: &EditRunConfig{
			Schema:                       json.RawMessage(`{"fields":[]}`),
			Instructions:                 "fill carefully",
			SchemaGenerationInstructions: "ignore signatures",
			AdvancedOptions:              &EditAdvancedOptions{NativeFieldsOnly: &tru, FlattenPdf: &tru},
		},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	g := string(got)
	for _, want := range []string{
		`"file":{"id":"file_x"}`,
		`"config":{`,
		`"schema":{"fields":[]}`,
		`"instructions":"fill carefully"`,
		`"schemaGenerationInstructions":"ignore signatures"`,
		`"advancedOptions":{`,
		`"nativeFieldsOnly":true`,
		`"flattenPdf":true`,
	} {
		if !strings.Contains(g, want) {
			t.Errorf("missing %q in body: %s", want, g)
		}
	}
	for _, bad := range []string{`"edit"`, `"values"`, `"priority"`, `"metadata"`} {
		if strings.Contains(g, bad) {
			t.Errorf("unwanted key %q in body: %s", bad, g)
		}
	}
}

func TestEditRunDecodesFullResponse(t *testing.T) {
	body := []byte(`{
		"object":"edit_run",
		"id":"edr_x",
		"status":"PROCESSED",
		"file":{"id":"file_in","object":"file","name":"in.pdf"},
		"config":{
			"schema":{"fields":[]},
			"instructions":"do it",
			"advancedOptions":{"flattenPdf":true}
		},
		"output":{"editedFile":{"id":"file_out","presignedUrl":"https://s3.example/abc"}},
		"metrics":{
			"processingTimeMs":1000,"pageCount":3,"fieldCount":7,
			"fieldsDetectedCount":7,"fieldsAnnotatedCount":7,
			"fieldDetectionTimeMs":100,"fieldAnnotationTimeMs":200,"fieldFillingTimeMs":300
		},
		"usage":{"credits":0.42},
		"failureReason":null,
		"failureMessage":null
	}`)
	var r EditRun
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ID != "edr_x" {
		t.Errorf("ID = %q, want edr_x", r.ID)
	}
	if r.File == nil || r.File.ID != "file_in" {
		t.Errorf("File missing or wrong id: %+v", r.File)
	}
	if r.Config == nil || r.Config.Instructions != "do it" {
		t.Errorf("Config.Instructions = %+v", r.Config)
	}
	if r.Output == nil || r.Output.EditedFile == nil || r.Output.EditedFile.ID != "file_out" {
		t.Errorf("Output.EditedFile = %+v", r.Output)
	}
	if r.Metrics == nil || r.Metrics.PageCount != 3 || r.Metrics.FieldCount != 7 {
		t.Errorf("Metrics = %+v", r.Metrics)
	}
	if r.Metrics.FieldFillingTimeMs != 300 {
		t.Errorf("Metrics.FieldFillingTimeMs = %d, want 300", r.Metrics.FieldFillingTimeMs)
	}
	if r.Usage == nil || r.Usage.Credits != 0.42 {
		t.Errorf("Usage = %+v", r.Usage)
	}
}

func TestBatchKindFromIDRecognizesWorkflowBatch(t *testing.T) {
	cases := map[string]BatchKind{
		"bpr_abc":   BatchKindProcessor,
		"bpar_abc":  BatchKindParse,
		"batch_abc": BatchKindWorkflow,
	}
	for in, want := range cases {
		got, ok := BatchKindFromID(in)
		if !ok {
			t.Errorf("BatchKindFromID(%q) ok=false", in)
			continue
		}
		if got != want {
			t.Errorf("BatchKindFromID(%q) = %q, want %q", in, got, want)
		}
	}
	if _, ok := BatchKindFromID("unknown_abc"); ok {
		t.Error("BatchKindFromID should return ok=false for unknown prefix")
	}
}

func TestWorkflowBatchInputOmitsPriorityAndMetadata(t *testing.T) {
	in := CreateWorkflowBatchInput{
		Workflow: &WorkflowRef{ID: "workflow_abc"},
		Inputs: []WorkflowBatchItem{
			{File: FileRef{ID: "file_a"}, Metadata: map[string]any{"k": "v"}},
		},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Decode into a generic map so we can verify only top-level keys exist.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatalf("unmarshal top: %v", err)
	}
	wantKeys := map[string]bool{"workflow": true, "inputs": true}
	for k := range top {
		if !wantKeys[k] {
			t.Errorf("unexpected top-level key %q in workflow batch body: %s", k, got)
		}
	}
	// Per-input metadata IS allowed and must round-trip.
	var unwrapped struct {
		Inputs []struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(got, &unwrapped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if unwrapped.Inputs[0].Metadata["k"] != "v" {
		t.Errorf("per-input metadata.k = %v, want v", unwrapped.Inputs[0].Metadata["k"])
	}
}

func TestWorkflowBatchResponseDecodes(t *testing.T) {
	body := []byte(`{"batchId":"batch_abc"}`)
	var resp WorkflowBatchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.BatchID != "batch_abc" {
		t.Errorf("BatchID = %q, want batch_abc", resp.BatchID)
	}
}

func TestCreateWorkflowRunInputHasNoFilesArray(t *testing.T) {
	// The server only accepts singular `file` for single workflow runs;
	// guard against accidentally re-introducing a `files` array.
	in := CreateWorkflowRunInput{
		Workflow: &WorkflowRef{ID: "workflow_x"},
		File:     &FileRef{ID: "file_a"},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(got), `"files"`) {
		t.Errorf("workflow run input must not have files[]: %s", got)
	}
	if !strings.Contains(string(got), `"file":{"id":"file_a"}`) {
		t.Errorf("workflow run input must use singular file: %s", got)
	}
}

func TestCreateWorkflowRunInputTypedOutputsAndSecrets(t *testing.T) {
	in := CreateWorkflowRunInput{
		Workflow: &WorkflowRef{ID: "workflow_x"},
		File:     &FileRef{ID: "file_a"},
		Outputs: []WorkflowProvidedOutput{
			{ProcessorID: "ex_root", Output: json.RawMessage(`{"value":{"k":"v"}}`)},
		},
		Secrets: map[string]any{"API_KEY": "secret"},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	g := string(got)
	if !strings.Contains(g, `"outputs":[{"processorId":"ex_root","output":{"value":{"k":"v"}}}]`) {
		t.Errorf("typed outputs missing: %s", g)
	}
	if !strings.Contains(g, `"secrets":{"API_KEY":"secret"}`) {
		t.Errorf("typed secrets missing: %s", g)
	}
}

func TestParseChunkDecodesBlocksAndMetadata(t *testing.T) {
	body := []byte(`{
		"id":"chunk_1","object":"chunk","type":"page","content":"hello",
		"metadata":{"pageRange":{"start":1,"end":1}},
		"blocks":[
			{
				"id":"blk_1","object":"block","type":"text","content":"hello",
				"polygon":[{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":0.1},{"x":0,"y":0.1}],
				"boundingBox":{"top":0,"left":0,"width":1,"height":0.1},
				"metadata":{}
			}
		]
	}`)
	var c ParseChunk
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Metadata == nil || c.Metadata.PageRange == nil || c.Metadata.PageRange.Start != 1 {
		t.Errorf("Metadata.PageRange decoded incorrectly: %+v", c.Metadata)
	}
	if len(c.Blocks) != 1 || c.Blocks[0].ID != "blk_1" {
		t.Errorf("Blocks decoded incorrectly: %+v", c.Blocks)
	}
	if len(c.Blocks[0].Polygon) != 4 {
		t.Errorf("Polygon should have 4 points, got %d", len(c.Blocks[0].Polygon))
	}
	if c.Blocks[0].BoundingBox.Width != 1 {
		t.Errorf("BoundingBox.Width = %v, want 1", c.Blocks[0].BoundingBox.Width)
	}
}

func TestParseRunOutputDecodesOCRWords(t *testing.T) {
	body := []byte(`{"chunks":[],"ocr":{"words":[{"content":"hello","boundingBox":{"top":0,"left":0,"width":0.1,"height":0.05},"confidence":0.99,"pageNumber":1}]}}`)
	var o ParseRunOutput
	if err := json.Unmarshal(body, &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.OCR == nil || len(o.OCR.Words) != 1 {
		t.Errorf("OCR.Words decoded incorrectly: %+v", o.OCR)
	}
	if o.OCR.Words[0].Confidence != 0.99 {
		t.Errorf("Confidence = %v, want 0.99", o.OCR.Words[0].Confidence)
	}
}

func TestChunkingStrategyOptionsRoundTrip(t *testing.T) {
	mn, mx := 100, 4000
	in := ChunkingStrategy{
		Type:    "section",
		Options: &ChunkingStrategyOptions{MinCharacters: &mn, MaxCharacters: &mx},
	}
	got, _ := json.Marshal(in)
	want := `{"type":"section","options":{"minCharacters":100,"maxCharacters":4000}}`
	requireJSONEqual(t, got, []byte(want), "ChunkingStrategy with options")
}

func TestParseConfigPassesBlockOptionsAndAdvancedOptionsThrough(t *testing.T) {
	in := ParseConfig{
		Target:          "markdown",
		BlockOptions:    json.RawMessage(`{"figures":{"enabled":true}}`),
		AdvancedOptions: json.RawMessage(`{"returnOcr":{"words":true},"alwaysConvertToPdf":true}`),
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	g := string(got)
	if !strings.Contains(g, `"blockOptions":{"figures":{"enabled":true}}`) {
		t.Errorf("blockOptions missing/mismatched: %s", g)
	}
	if !strings.Contains(g, `"advancedOptions":{"returnOcr":{"words":true},"alwaysConvertToPdf":true}`) {
		t.Errorf("advancedOptions missing/mismatched: %s", g)
	}
}

func TestExtractOutputRoundTripsBothShapes(t *testing.T) {
	// The server returns either the new {value, metadata} envelope or a
	// legacy flat field-name->result map. Both must pass through the CLI's
	// JSON output verbatim.
	cases := map[string]string{
		"new shape":    `{"value":{"invoice_id":"INV-1"},"metadata":{"invoice_id":{"ocrConfidence":0.99}}}`,
		"legacy shape": `{"invoice_id":{"id":"f1","value":"INV-1","confidence":0.99}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var o ExtractOutput
			if err := json.Unmarshal([]byte(body), &o); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, err := json.Marshal(o)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != body {
				t.Errorf("round-trip mismatch:\n  got:  %s\n  want: %s", got, body)
			}
		})
	}
}

func TestWorkflowStepRunResultDispatch(t *testing.T) {
	body := []byte(`{
		"id":"step_run_1","object":"workflow_step_run","status":"PROCESSED",
		"step":{"id":"step_1","object":"workflow_step","name":"extract","type":"EXTRACT"},
		"result":{"extractRun":{"id":"exr_1","object":"extract_run","status":"PROCESSED"}}
	}`)
	var s WorkflowStepRun
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok, err := s.ExtractResult()
	if err != nil {
		t.Fatalf("ExtractResult error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for EXTRACT step")
	}
	if r.ExtractRun == nil || r.ExtractRun.ID != "exr_1" {
		t.Errorf("nested ExtractRun missing: %+v", r)
	}

	// Wrong-type accessor returns ok=false.
	if _, ok, _ := s.ParseResult(); ok {
		t.Error("ParseResult should return ok=false for non-PARSE step")
	}
}

func TestWorkflowStepRunRuleValidationResult(t *testing.T) {
	body := []byte(`{
		"id":"step_run_2","object":"workflow_step_run","status":"PROCESSED",
		"step":{"id":"step_2","object":"workflow_step","name":"validate","type":"RULE_VALIDATION"},
		"result":{
			"rules":[
				{"name":"non_empty","valid":true},
				{"name":"format","valid":false,"failureReason":"RULE_FAILED","error":"bad date"}
			],
			"allPassed":false
		}
	}`)
	var s WorkflowStepRun
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok, err := s.RuleValidationResult()
	if err != nil {
		t.Fatalf("RuleValidationResult error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if r.AllPassed {
		t.Error("AllPassed should be false")
	}
	if len(r.Rules) != 2 || r.Rules[1].FailureReason != "RULE_FAILED" {
		t.Errorf("Rules decoded incorrectly: %+v", r.Rules)
	}
}

func TestExtractBatchInputOmitsTopLevelMetadata(t *testing.T) {
	in := CreateExtractBatchInput{
		Extractor: &ExtractorRef{ID: "ex_x"},
		Inputs: []ProcessorBatchItem{
			{File: FileRef{ID: "file_a"}, Metadata: map[string]any{"k": "v"}},
		},
	}
	got, _ := json.Marshal(in)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(got, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allowed := map[string]bool{"extractor": true, "inputs": true, "priority": true}
	for k := range top {
		if !allowed[k] {
			t.Errorf("unexpected top-level key %q in extract batch body: %s", k, got)
		}
	}
}

func TestParseBatchInputOmitsTopLevelMetadata(t *testing.T) {
	in := CreateParseBatchInput{
		Inputs: []ParseBatchItem{{File: FileRef{ID: "file_a"}}},
		Config: &ParseConfig{Target: "markdown"},
	}
	got, _ := json.Marshal(in)
	if strings.Contains(string(got), `"metadata"`) && !strings.Contains(string(got), `"inputs":[{"file":{"id":"file_a"}`) {
		t.Errorf("parse batch body has top-level metadata or wrong shape: %s", got)
	}
}

func TestProcessorBatchItemOnlyHasFileAndMetadata(t *testing.T) {
	in := ProcessorBatchItem{
		File:     FileRef{ID: "file_a"},
		Metadata: map[string]any{"k": "v"},
	}
	got, _ := json.Marshal(in)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(got, &keys)
	allowed := map[string]bool{"file": true, "metadata": true}
	for k := range keys {
		if !allowed[k] {
			t.Errorf("unexpected key %q in batch item: %s", k, got)
		}
	}
}

func TestFileDecodesContentsAndParentSplit(t *testing.T) {
	body := []byte(`{
		"object":"file","id":"file_x","name":"a.pdf","type":"pdf",
		"presignedUrl":"https://s3/abc",
		"contents":{
			"rawText":"hello",
			"markdown":"# Title",
			"pages":[{"pageNumber":1,"rawText":"hello","markdown":"# Title"}],
			"sections":[{"startPageNumber":1,"endPageNumber":1,"markdown":"# Title"}]
		},
		"metadata":{
			"pageCount":2,
			"parentSplit":{"id":"split_1","type":"invoice","identifier":"INV-1","startPage":1,"endPage":2}
		},
		"createdAt":"2026-01-01","updatedAt":"2026-01-01"
	}`)
	var f File
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Contents == nil || f.Contents.RawText != "hello" {
		t.Errorf("Contents.RawText decoded incorrectly: %+v", f.Contents)
	}
	if len(f.Contents.Pages) != 1 || f.Contents.Pages[0].Markdown != "# Title" {
		t.Errorf("Contents.Pages decoded incorrectly: %+v", f.Contents.Pages)
	}
	if f.Metadata.ParentSplit == nil || f.Metadata.ParentSplit.Identifier != "INV-1" {
		t.Errorf("ParentSplit decoded incorrectly: %+v", f.Metadata.ParentSplit)
	}
}

func TestFileRefBase64NameAndSettingsRoundTrip(t *testing.T) {
	ref := FileRef{
		Base64: "aGVsbG8=",
		Name:   "hello.pdf",
	}
	got, _ := json.Marshal(ref)
	if !strings.Contains(string(got), `"base64":"aGVsbG8="`) {
		t.Errorf("missing base64: %s", got)
	}
	if !strings.Contains(string(got), `"name":"hello.pdf"`) {
		t.Errorf("missing name: %s", got)
	}

	withPassword := FileRef{URL: "https://x/y.pdf", Settings: &FileSettings{Password: "secret"}}
	got2, _ := json.Marshal(withPassword)
	if !strings.Contains(string(got2), `"settings":{"password":"secret"}`) {
		t.Errorf("missing settings.password: %s", got2)
	}
}

func TestExtractorDecodesDraftVersionWithConfig(t *testing.T) {
	body := []byte(`{
		"object":"extractor","id":"ex_abc","name":"Invoices",
		"createdAt":"2026-01-01","updatedAt":"2026-01-01",
		"draftVersion":{
			"object":"extractor_version","id":"exv_abc","version":"3",
			"description":"v3 changes","extractorId":"ex_abc","createdAt":"2026-01-01",
			"config":{"fields":[{"key":"invoice_id","type":"string"}]}
		}
	}`)
	var e Extractor
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.DraftVersion == nil {
		t.Fatal("DraftVersion should be populated")
	}
	if e.DraftVersion.ExtractorID != "ex_abc" {
		t.Errorf("DraftVersion.ExtractorID = %q, want ex_abc", e.DraftVersion.ExtractorID)
	}
	if !strings.Contains(string(e.DraftVersion.Config), `"key":"invoice_id"`) {
		t.Errorf("DraftVersion.Config missing field, got %s", e.DraftVersion.Config)
	}
}

func TestWorkflowDecodesDraftVersionWithSteps(t *testing.T) {
	body := []byte(`{
		"object":"workflow","id":"workflow_abc","name":"Pipeline",
		"createdAt":"2026-01-01","updatedAt":"2026-01-01",
		"draftVersion":{
			"object":"workflow_version","id":"wfv_abc","version":"5",
			"name":"prod","createdAt":"2026-01-01",
			"steps":[{"id":"step_1","name":"parse","type":"PARSE"}]
		}
	}`)
	var w Workflow
	if err := json.Unmarshal(body, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.DraftVersion == nil {
		t.Fatal("DraftVersion should be populated")
	}
	if w.DraftVersion.Name != "prod" {
		t.Errorf("DraftVersion.Name = %q, want prod", w.DraftVersion.Name)
	}
	if !strings.Contains(string(w.DraftVersion.Steps), `"type":"PARSE"`) {
		t.Errorf("DraftVersion.Steps missing PARSE step, got %s", w.DraftVersion.Steps)
	}
}

func TestProcessorListResponseHasNoDraftVersion(t *testing.T) {
	// List endpoints return the slim Summary form. Confirm DraftVersion is
	// nil when not present on the wire (not silently zeroed into the wrong
	// shape).
	body := []byte(`{"object":"extractor","id":"ex_abc","name":"X","createdAt":"2026-01-01","updatedAt":"2026-01-01"}`)
	var e Extractor
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.DraftVersion != nil {
		t.Errorf("DraftVersion should be nil for list responses, got %+v", e.DraftVersion)
	}
}

func TestEvaluationItemDecodesFileAndExpectedOutput(t *testing.T) {
	body := []byte(`{
		"object":"evaluation_set_item",
		"id":"esi_x",
		"evaluationSetId":"ev_x",
		"file":{"id":"file_1","object":"file","name":"a.pdf"},
		"expectedOutput":{"value":{"k":"v"}}
	}`)
	var it EvaluationItem
	if err := json.Unmarshal(body, &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it.File == nil || it.File.ID != "file_1" {
		t.Errorf("File missing or wrong id: %+v", it.File)
	}
	if it.ExpectedOutput == nil {
		t.Error("ExpectedOutput should not be nil")
	}
}

func TestEvaluationItemHasNoStaleFields(t *testing.T) {
	// The server transform for PublicEvaluationSetItem returns
	// {object, id, evaluationSetId, file, expectedOutput} — no input/output
	// or timestamps. Guard against accidentally re-adding them.
	it := EvaluationItem{ID: "esi_x", Object: "evaluation_set_item"}
	got, _ := json.Marshal(it)
	for _, bad := range []string{"input", "output", "createdAt", "updatedAt"} {
		// Use a quoted-key match so substrings of legit keys (e.g. "expectedOutput")
		// don't false-positive.
		key := `"` + bad + `"`
		if strings.Contains(string(got), key) {
			t.Errorf("EvaluationItem should not expose %s; got %s", key, got)
		}
	}
}

func TestEvaluationItemsCreateResponseDecodesEnvelope(t *testing.T) {
	body := []byte(`{"evaluationSetItems":[{"id":"esi_1","object":"evaluation_set_item"},{"id":"esi_2","object":"evaluation_set_item"}]}`)
	var resp EvaluationItemsCreateResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.EvaluationSetItems) != 2 {
		t.Errorf("len = %d, want 2", len(resp.EvaluationSetItems))
	}
}

func TestEvaluationRunDecodesEntityAndMetrics(t *testing.T) {
	body := []byte(`{
		"object":"evaluation_set_run",
		"id":"esr_x",
		"entity":{"object":"extractor","id":"ex_x","name":"Extractor","createdAt":"2026-01-01","updatedAt":"2026-01-01"},
		"entityVersion":{"object":"extractor_version","id":"exv_x","version":"3","extractorId":"ex_x"},
		"status":"PROCESSED",
		"metrics":{"numFiles":10,"numPages":20,"meanLatencyMs":123.4,"p99LatencyMs":456.7}
	}`)
	var r EvaluationRun
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Entity == nil || r.Entity.ID != "ex_x" {
		t.Errorf("Entity missing or wrong id: %+v", r.Entity)
	}
	if r.Metrics == nil || r.Metrics.NumFiles == nil || *r.Metrics.NumFiles != 10 {
		t.Errorf("Metrics.NumFiles missing or wrong: %+v", r.Metrics)
	}
	if r.Metrics.MeanLatencyMs == nil || *r.Metrics.MeanLatencyMs != 123.4 {
		t.Errorf("Metrics.MeanLatencyMs missing or wrong: %+v", r.Metrics.MeanLatencyMs)
	}
}

func TestWorkflowRunMetadataAsMap(t *testing.T) {
	body := []byte(`{"id":"workflow_run_x","object":"workflow_run","status":"PROCESSED","metadata":{"foo":"bar","n":42}}`)
	var r WorkflowRun
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Metadata["foo"] != "bar" {
		t.Errorf("Metadata.foo = %v, want bar", r.Metadata["foo"])
	}
	// Round-trip through marshal to ensure the typed map preserves shape.
	got, _ := json.Marshal(r)
	if !strings.Contains(string(got), `"metadata":{`) {
		t.Errorf("marshalled metadata missing: %s", got)
	}
}

func TestEditRunHasNoCreatedAtUpdatedAt(t *testing.T) {
	// PublicEditRun does not include createdAt/updatedAt. If that changes,
	// this guard fails and the struct should be re-extended.
	r := EditRun{ID: "edr_x", Status: StatusProcessed}
	got, _ := json.Marshal(r)
	if strings.Contains(string(got), "createdAt") || strings.Contains(string(got), "updatedAt") {
		t.Errorf("EditRun should not expose createdAt/updatedAt, got %s", got)
	}
}

func TestGenerateEditSchemaInputNestsConfig(t *testing.T) {
	tru := true
	in := GenerateEditSchemaInput{
		File: FileRef{ID: "file_x"},
		Config: &EditSchemaGenerationConfig{
			InputSchema:     json.RawMessage(`{"$schema":"https://json-schema.org/draft-07/schema#"}`),
			Instructions:    "look at signatures",
			AdvancedOptions: &EditAdvancedOptions{NativeFieldsOnly: &tru},
		},
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	g := string(got)
	if !strings.Contains(g, `"config":{`) {
		t.Errorf("config must be nested; got %s", g)
	}
	if !strings.Contains(g, `"inputSchema":{`) {
		t.Errorf("inputSchema must be inside config (not 'schema'); got %s", g)
	}
	if strings.Contains(g, `"nativeFieldsOnly"`) && !strings.Contains(g, `"advancedOptions"`) {
		t.Errorf("nativeFieldsOnly must be inside advancedOptions; got %s", g)
	}
}
