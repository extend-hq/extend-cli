package cli

// Field catalogs surfaced in `--help` for the JSON objects the CLI takes
// as a single flag: the action verbs' `--config` (extract/classify/split),
// the create/update request bodies (extractors/classifiers/splitters/
// workflows `--from-file`), parse's `--block-options`/`--advanced-options`,
// and the `extend_edit:*` annotations on an edit schema.
//
// Per the documentation contract in internal/cli/AGENTS.md ("Flags vs.
// JSON config"): config objects ride in a single JSON flag rather than
// per-field flags, and discoverability is satisfied by documenting the
// field catalog in Details. These constants are that catalog.
//
// Source of truth is the pinned extend-go-sdk structs: SplitConfig,
// ExtractConfigJSON, ClassifyConfig, WorkflowsCreateRequest.Steps,
// ParseConfigBlockOptions, ParseConfigAdvancedOptions, and EditJSON. When
// the SDK is regenerated and a field is added, removed, or renamed, update
// the matching catalog here in the same change — drift is a defect, not a
// follow-up. The drift guards in configdocs_test.go enforce this.

// splitConfigFields documents the SplitConfig object (split --config and
// the `config` of a splitters create/update body).
const splitConfigFields = `  - baseProcessor (string) — "splitting_performance" (default) or "splitting_light".
  - baseVersion (string) — pin a base-processor version; defaults to the latest stable.
  - splitClassifications (array, required) — the document types to split into. Provide
    at least one entry, and at least one entry must have type "other". Each entry:
      - id (string) — unique; lowercase_snake_case recommended.
      - type (string) — the segment type label echoed back in the output splits.
      - description (string) — what a section of this type looks like.
      - identifierKey (string, optional) — natural-language rule for extracting a
        per-subdocument identifier (e.g. an invoice number) so related segments can
        be merged (splitting_light >= 1.3.0 / splitting_performance >= 1.5.0).
  - splitRules (string) — natural-language guidance on where to split and how to group pages.
  - advancedOptions (object) — splitExcelDocumentsBySheetEnabled, pageOverlapEnabled,
    pageRanges. (Legacy splitIdentifierRules/splitMethod are accepted but ignored on
    current base versions.)
  - parseConfig (object) — parsing options applied before splitting; see ` + "`extend parse --help`" + `.`

// extractConfigFields documents the ExtractConfigJSON object (extract
// --config and the `config` of an extractors create/update body).
const extractConfigFields = `  - baseProcessor (string) — "extraction_performance" (default) or "extraction_light".
  - baseVersion (string) — pin a base-processor version; defaults to the latest stable.
  - schema (object, required) — JSON Schema of the data to extract.
  - extractionRules (string) — natural-language guidance for the extraction.
  - advancedOptions (object) — citationsEnabled, citationMode (line|word|block),
    arrayCitationStrategy, advancedMultimodalEnabled, modelReasoningInsightsEnabled,
    arrayStrategy, chunkingOptions, pageRanges, reviewAgent, currentDateEnabled,
    excelSheetRanges, excelSheetSelectionStrategy.
  - parseConfig (object) — parsing options; see ` + "`extend parse --help`" + `.`

// classifyConfigFields documents the ClassifyConfig object (classify
// --config and the `config` of a classifiers create/update body).
const classifyConfigFields = `  - baseProcessor (string) — "classification_performance" (default) or "classification_light".
  - baseVersion (string) — pin a base-processor version; defaults to the latest stable.
  - classifications (array, required) — the categories. Provide at least one entry, and
    at least one entry must have type "other". Each entry: id, type, description.
  - classificationRules (string) — natural-language guidance for the classification.
  - advancedOptions (object) — advancedMultimodalEnabled, memoryEnabled, context, pageRanges.
  - parseConfig (object) — parsing options; see ` + "`extend parse --help`" + `.`

// workflowStepsFields documents the steps array of a workflows
// create/update body. Workflows have no inline action-verb config, so
// this is used only by the create/update body docs.
const workflowStepsFields = `  - steps (array) — the workflow's processing graph; omit to get the default
    TRIGGER -> PARSE steps. Each step has a "type", a unique "name", and optional
    "next" entries that route to downstream steps. Step types:
      TRIGGER, PARSE, EXTRACT, CLASSIFY, SPLIT, MERGE_EXTRACT, CONDITIONAL,
      CONDITIONAL_EXTRACT, EXTERNAL_DATA_VALIDATION, RULE_VALIDATION,
      WEBHOOK_RESPONSE, HUMAN_REVIEW, COLLECT, FILE_CONVERSION.
    See the "Configuring Workflows via API" guide for per-type fields and branching patterns.`

// actionConfigDoc wraps a bare field catalog with the lead-in shown under
// an action verb's --config flag (extract/classify/split). The object is
// passed directly to --config (no name envelope).
func actionConfigDoc(fields string) string {
	return "The --config object (a complete standalone config) accepts these fields:\n\n" + fields
}

// processorBodyDoc wraps a bare config field catalog with the create/update
// request-body envelope: a JSON object with "name" plus a "config" object.
// Used by extractors/classifiers/splitters.
func processorBodyDoc(fields string) string {
	return `The request body is a JSON object:

    { "name": "<name>", "config": { … } }

The config object accepts these fields:

` + fields
}

// workflowBodyDoc is the workflow create/update request-body documentation:
// a JSON object with "name" plus a "steps" array.
const workflowBodyDoc = `The request body is a JSON object:

    { "name": "<name>", "steps": [ … ] }

` + workflowStepsFields

// parseBlockOptionsFields documents the ParseConfigBlockOptions object
// (parse --block-options). Each top-level key is a nested object.
const parseBlockOptionsFields = `  - figures (object) — enabled; figureImageClippingEnabled (clip each figure to an image);
    advancedChartExtractionEnabled (extract the underlying chart data).
  - tables (object) — enabled; targetFormat ("markdown"|"html"); tableHeaderContinuationEnabled
    (carry headers across page breaks); cellBlocksEnabled (emit a block per cell);
    agentic (object: enabled, customInstructions).
  - text (object) — signatureDetectionEnabled; agentic (object: enabled, customInstructions).
  - keyValue (object) — blankFieldFormattingEnabled (emit detected blank fields).
  - barcodes (object) — readingEnabled (decode barcode values); imageClippingEnabled.
  - formulas (object) — enabled (detect formulas and include their LaTeX representation).`

// parseAdvancedOptionsFields documents the ParseConfigAdvancedOptions
// object (parse --advanced-options).
const parseAdvancedOptionsFields = `  - pageRotationEnabled (bool) — auto-detect and correct rotated pages.
  - pageRanges (array) — restrict parsing to page ranges; each entry { start, end }.
  - excelParsingMode ("basic"|"advanced") — "advanced" enables layout block detection for complex spreadsheets; .xls files always use "basic".
  - excelSkipHiddenContent (bool) — drop hidden rows, columns, and sheets.
  - excelUseRawCellValues (bool) — emit raw calculated values instead of locale-formatted ones.
  - excelSkipCalculation (bool) — skip formula recalculation (faster; disable for volatile NOW()/TODAY()).
  - verticalGroupingThreshold (number 0.1–5.0, default 1.0) — line-grouping sensitivity; --target spatial only.
  - returnOcr (object) — words (bool): include word-level raw OCR data in the response.
  - alwaysConvertToPdf (bool) — convert images/Office/HTML to PDF first (enables spatial bboxes).
  - enrichmentFormat ("xml"|"bracket") — annotation style, e.g. <page_number>1</page_number> vs [page_number: 1].
  - imageConversionQuality ("high"|"medium"|"low") — quality when converting to PDF.
  - formattingDetection (array) — change-tracking detection; each entry { "type": "change_tracking" }.
    Requires engine "parse_performance" >= 2.0.0.`

// parseOptionsDoc is the block-and-advanced options reference appended to
// `extend parse` Details so the JSON shapes are discoverable from --help.
func parseOptionsDoc() string {
	return "The --block-options object tunes per-block detection (each key is a nested object):\n\n" +
		parseBlockOptionsFields +
		"\n\nThe --advanced-options object tunes the parse itself:\n\n" +
		parseAdvancedOptionsFields
}

// editSchemaPropertyDoc documents the Extend-specific extend_edit:* keys
// the schema generator annotates each field with (EditJSON in the SDK). It
// is surfaced in `extend edit schema generate` --help so an agent knows
// which keys to populate and which to leave as generated.
const editSchemaPropertyDoc = `A generated schema is JSON Schema with Extend-specific extend_edit:* annotations on
each field. Populate the value keys; leave the structural keys as generated (inspect
the schema — do not invent them):

  - extend_edit:value (any) — the value to fill into this field. Omit to let the server
    infer one from --instructions.
  - extend_edit:image (object) — image fill for signature fields (PNG or JPEG URL only).
  - extend_edit:field_type (string) — the PDF control: text, signature, checkbox, radio,
    dropdown, optionList, or table (allowed values depend on the field's JSON type).
  - extend_edit:bbox / extend_edit:bboxes — placement box(es); bboxes pairs one box per
    enum option (index i ↔ enum option i).
  - extend_edit:page_index (int) — zero-based page the field sits on.
  - extend_edit:text_edit_options (object) — text styling for the filled value.
  - extend_edit:column_width (number) / extend_edit:row_heights (array) — table cell sizing,
    as percentages.

Standard JSON Schema keys carry through: type, properties, items, enum, and maxItems
(the row cap for array/table fields).`
