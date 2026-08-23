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
const extractConfigFields = `  - schema (object, required) - JSON Schema of the data to extract. The root
    must be an object. Allowed types: string, number, integer, boolean, object,
    array. Primitive fields must be nullable, e.g. "type": ["string", "null"];
    primitive array items are not nullable. Enums must contain only strings plus
    null. Maximum nesting is 5 levels. Property keys may contain letters,
    numbers, underscores, and hyphens. Unsupported: anyOf/oneOf/allOf, schema
    definitions, recursive schemas, regex/type-specific validation keywords,
    conditional validation, and const.
  - schema custom keys - use "extend:type" for typed fields: "date" on nullable
    strings (ISO yyyy-mm-dd output). Currency fields are objects, not primitive
    numbers: set "type":"object", "extend:type":"currency", and properties
    amount (nullable number) plus iso_4217_currency_code (nullable string).
    Signature fields are objects with printed_name, signature_date, is_signed,
    and title_or_role. Use "extend:name" to give the model a more descriptive
    field name without changing the output key. Use "extend:descriptions" to
    describe enum options.
  - schema descriptions - write direct, contextual descriptions. Prefer
    descriptive field names such as invoice_number or provider_email over vague
    names like number or email. Mention what the value means, where it usually
    appears, and any formatting expectations. Use arrays for repeated rows like
    line_items and nested objects for grouped data.
  - baseProcessor (string) - "extraction_performance" (default) for highest
    accuracy, complex layouts/tables, handwriting, and multimodal content;
    "extraction_light" for faster, cheaper extraction on simple documents.
    extraction_light does not return logprobsConfidence and lacks advanced
    visual features such as figure parsing and signature detection.
  - baseVersion (string) - pin a base-processor version such as "4.6.0";
    omit to use the latest stable version.
  - extractionRules (string) - natural-language guidance applied across the
    whole extraction. Use it to disambiguate fields, set formats, and encode
    business logic, e.g. "If multiple totals appear, use the grand total. Return
    dates in ISO 8601 format."
  - advancedOptions.citationsEnabled (bool) - return bounding-box citations and
    source text for extracted values. Useful for review/highlighting, but adds
    latency; disable when spatial references are not needed.
  - advancedOptions.citationMode ("line"|"word"|"block", default "line") -
    citation granularity when citationsEnabled is true. word is more precise;
    block has higher recall and lower granularity.
  - advancedOptions.arrayCitationStrategy ("item"|"property") - citation
    granularity for arrays. property requires extraction_performance >= 4.4.0.
  - advancedOptions.advancedMultimodalEnabled (bool) - use vision-language
    understanding for scans, handwriting, checks, forms, and poor images. Disable
    for clean digital PDFs when latency matters.
  - advancedOptions.modelReasoningInsightsEnabled (bool) - include reasoning
    insights in output.metadata[].insights for debugging; disable in production
    or latency-sensitive runs unless needed.
  - advancedOptions.reviewAgent.enabled (bool) - ask the Review Agent to score
    each value with reviewAgentScore 1-5 and add issue/review_summary insights.
  - advancedOptions.currentDateEnabled (bool) - include the current date as
    context for date-sensitive extraction.
  - advancedOptions.arrayStrategy.type - large-array handling for hundreds of
    rows: large_array_heuristics (lower latency), large_array_max_context
    (highest accuracy, higher cost), or large_array_overlap_context (keeps page
    context around chunks). Omit unless extracting very large arrays.
  - advancedOptions.chunkingOptions - tune large-document chunking and merging:
    chunkingStrategy (standard|semantic), pageChunkSize, chunkSelectionStrategy
    (intelligent|confidence|take_first|take_last), customSemanticChunkingRules.
    intelligent is slowest; confidence/take_first/take_last reduce latency.
  - advancedOptions.pageRanges (array) - 1-based inclusive page ranges such as
    [{"start":1,"end":5}]. Use when relevant data is on known pages to reduce
    cost and latency.
  - advancedOptions.excelSheetSelectionStrategy ("intelligent"|"all"|"first"|"last")
    and advancedOptions.excelSheetRanges - choose workbook sheets/ranges.
  - parseConfig (object) - parsing options applied before extraction; see ` + "`extend parse --help`" + `.
    Reach for this when Parse misses or mangles a value (for example, enable
    text.agentic OCR for messy scans, table header continuation for long tables,
    or signature detection for signatures). Extract can only return values that
    Parse can see.`

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
const workflowStepsFields = `  - steps (array) - the workflow's processing graph. Omit on create to make
    a named draft, then update the draft with steps before deploying.

    Lifecycle:
      1. create a workflow draft: extend workflows create --from-file body.json
      2. update the draft graph: extend workflows update workflow_xxx --from-file steps.json
      3. deploy an immutable version: extend workflows versions create workflow_xxx --name "initial"
      4. run it async: extend workflows run invoice.pdf --using workflow_xxx [--wait]

    Step envelope:
      { "name": "extract", "type": "EXTRACT", "config": { ... }, "next": [{ "step": "review" }] }

    Every step has a unique "name", a "type", optional "config", and optional
    "next" routes. Every runnable workflow starts with TRIGGER followed by
    PARSE. WEBHOOK_RESPONSE is terminal and must not have "next".

    Minimal linear extraction graph:

      [
        { "name": "trigger", "type": "TRIGGER", "next": [{ "step": "parse" }] },
        { "name": "parse", "type": "PARSE", "next": [{ "step": "extract" }] },
        {
          "name": "extract",
          "type": "EXTRACT",
          "config": { "extractor": { "id": "ex_abc123", "version": "latest" } },
          "next": [{ "step": "webhook" }]
        },
        { "name": "webhook", "type": "WEBHOOK_RESPONSE" }
      ]

    Routing rules:
      - Simple steps use next entries shaped as { "step": "target_step" }.
      - CLASSIFY and SPLIT route with classificationId values. These must match
        classification IDs from the classifier/splitter config, not type labels.
      - CONDITIONAL routes with conditionId values matching config.conditions[].id.
      - RULE_VALIDATION routes with result values: "pass" or "fail".
      - Conditional and formula fields can reference prior processor outputs as
        "{{stepName.output.*}}", workflow run metadata as "{{metadata.*}}", and
        external validation responses as
        "{{externalDataValidationStep.output.response.data.*}}".

    Routing snippets:

      "next": [
        { "step": "extract_invoice", "classificationId": "cls_invoice" },
        { "step": "extract_receipt", "classificationId": "cls_receipt" }
      ]

      "next": [
        { "step": "review", "conditionId": "high_value" },
        { "step": "webhook", "conditionId": "default_path" }
      ]

      "next": [
        { "step": "webhook", "result": "pass" },
        { "step": "review", "result": "fail" }
      ]

    Processor references and versions:
      - EXTRACT config references { "extractor": { "id": "ex_xxx", "version": "latest" } }.
      - CLASSIFY config references { "classifier": { "id": "cl_xxx", "version": "0.1" } }.
      - SPLIT config references { "splitter": { "id": "spl_xxx", "version": "0.1" } }.
      - All processor references require an explicit version: "latest", "draft", or semver.
      - CLASSIFY and SPLIT cannot use "latest"; pin semver or use "draft" because
        classification IDs are version-specific.
      - EXTRACT and CONDITIONAL_EXTRACT can use "latest", "draft", or semver.

    Step types:
      - TRIGGER - single workflow entry point; routes to exactly one PARSE step.
      - PARSE - OCR/text extraction; must appear immediately after TRIGGER. By
        default, parser settings are inferred from downstream extract/classify/split
        processors. Set config.parseConfig (target, chunkingStrategy, blockOptions,
        advancedOptions, etc.) when you need explicit control or identical parsed
        content across every branch; the Parse step config takes precedence for all
        downstream processors.
      - EXTRACT - runs an extractor. Config is required before deploy; next cannot
        be set until config is present.
      - CLASSIFY - runs a classifier and routes by classificationId.
      - SPLIT - splits a multi-document file and routes each sub-document by classificationId.
      - MERGE_EXTRACT - combines multiple upstream extract outputs; config.mergeOrder
        controls overlapping-field priority.
      - CONDITIONAL - routes by IF/ELSE conditions over upstream output values, for
        example leftOperand "{{ extract.output.value.total }}" with operation GTE.
        Supported operation/operator values: EQUALS, GTE, LTE, IS_NULL, CONTAINS,
        and NO_OP for a branch with no comparison. Use conditionId in next routes.
        Extraction confidence can be referenced with "{{ extract.avgConfidence }}",
        "{{ extract.minConfidence }}", or field paths like
        "{{ extract.output.metadata.invoice_number.ocrConfidence }}". You can also
        branch on run metadata, e.g. "{{ metadata.provider_name }}", or an external
        validation response, e.g.
        "{{ external_validate.output.response.data.requires_review }}".
      - CONDITIONAL_EXTRACT - chooses an extractor by formula rules. Formula rules
        can use previous step outputs, external validation outputs, and run metadata;
        each rule points to an extractor reference. Include a final rule with formula
        "TRUE" as the default fallback so unmatched documents do not fail at runtime.
      - RULE_VALIDATION - checks boolean formulas against extracted data; route on
        result "pass" or "fail". Rules have name, optional description, and formula;
        the formula must evaluate to a boolean and may reference any earlier step
        output, including data fetched with EXTERNAL_DATA_VALIDATION. A common
        topology routes pass to WEBHOOK_RESPONSE and fail to HUMAN_REVIEW.
      - EXTERNAL_DATA_VALIDATION - posts extraction data to an external HTTP endpoint;
        config.requestOptions sets url, method, headers, and contentType, and
        failureBehavior controls failures. Extend also signs requests with the
        extend-signature header, using the same secret style as webhooks. Use the
        response in conditionals via "{{ external_validate.output.response.data }}".
      - HUMAN_REVIEW - pauses for dashboard review before continuing.
      - COLLECT - joins multiple upstream branches before continuing; commonly follows
        CLASSIFY or SPLIT branches.
      - FILE_CONVERSION - converts file format before downstream processing; config
        can set failureBehavior.
      - WEBHOOK_RESPONSE - terminal delivery step for workflow results; no "next".

    Common patterns:
      - Linear extraction: trigger -> parse -> extract -> webhook.
      - Human-in-the-loop: trigger -> parse -> extract -> review -> webhook.
      - Classify and route: trigger -> parse -> classify -> extractor branches -> review/webhook.
      - Split and collect: trigger -> parse -> split -> extractor/review branches -> collect -> webhook.
      - Conditional review: trigger -> parse -> extract -> conditional -> review or webhook.
      - Validation gate: trigger -> parse -> extract -> rule validation -> pass webhook or fail review.`

// actionConfigDoc wraps a bare field catalog with the lead-in shown under
// an action verb's --config flag (extract/classify/split). The object is
// passed directly to --config (no name envelope).
func actionConfigDoc(fields string) string {
	return "The --config object (a complete standalone config) accepts these fields:\n\n" + fields
}

const extractOutputDoc = `Extract output:

  - output.value - the extracted data, shaped exactly like your schema.
  - output.metadata - per-field details keyed by path notation such as
    invoice_number, line_items[0], or line_items[0].description.
  - metadata[field].ocrConfidence - OCR confidence from 0 to 1. Prefer this
    and Review Agent scores for new integrations.
  - metadata[field].logprobsConfidence - model confidence from token
    probabilities. This is being phased out; extraction_light never returns it
    and extraction_performance 4.6.0+ returns null.
  - metadata[field].reviewAgentScore - 1-5 when advancedOptions.reviewAgent is
    enabled.
  - metadata[field].citations - page, polygon, and referenceText when
    citationsEnabled is true. Coordinates use the page's point space with origin
    at the top-left; page.number is 1-based.
  - metadata[field].insights - reasoning/review notes when reasoning insights or
    the Review Agent are enabled.

Confidence and review routing:

  - High confidence is useful but not a guarantee of correctness; critical data
    should still be cross-checked.
  - For workflow conditionals, aggregate confidence variables are
    {{extractStep.avgConfidence}} and {{extractStep.minConfidence}}.
  - Specific fields can be referenced as
    {{extractStep.output.metadata.invoice_number.ocrConfidence}}.
  - Array/nested confidence paths are available in metadata output, but not as
    workflow conditional references today.`

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
  - excelIncludeCellMetadata (bool) — include cell provenance (source refs/formulas, data-cell/data-formula in HTML); advanced mode only.
  - excelIncludeCellFormatting (bool) — include cell formatting (bold, italic, font/background color, inline HTML styles); advanced mode only.
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
each field. Populate the value keys; leave detected structural keys as generated
(inspect the schema — do not invent field names or bounding boxes):

  - type (string or array) — JSON type. Allowed PDF controls depend on the type:
      ["string","null"] -> text or signature
      ["number","null"] / ["integer","null"] -> text
      ["boolean","null"] -> checkbox or radio
      "array" -> text or table
      "object" -> signature
      enum with no type -> radio, optionList, or dropdown
  - extend_edit:field_type (string) — PDF control: text, checkbox, radio, dropdown,
    optionList, signature, or table.
  - extend_edit:value (any) — value to fill into this field. Omit to let the server
    infer one from --instructions.
  - extend_edit:bbox (object) — placement box {left, top, right, bottom} in PDF pixel
    coordinates. Usually generated; do not hand-author unless you know the layout.
  - extend_edit:bboxes (array) — one placement box per radio enum option; enum index i
    corresponds to bboxes index i.
  - extend_edit:page_index (int) — zero-based page index where the field sits.
  - extend_edit:image (object) — signature image fill: {"image_url":"https://..."};
    PNG or JPEG only.
  - extend_edit:text_edit_options (object) — text styling: combing, maxLength,
    fontSize, fontColor (RGB 0-255), font.
  - extend_edit:column_width (number) — column width percentage for table fields.
  - extend_edit:row_heights (array) — row height percentages for array/table fields.

Standard JSON Schema keys carry through: description, properties, items, enum,
required, additionalProperties, and maxItems (row cap for array/table fields).
Root-level conditional form logic is supported with dependentRequired,
if/then/else, allOf, oneOf, anyOf, and not. Conditional clauses do not accept
extend_edit:* keys.`

const editOutputDoc = `Edit output:

  - output.editedFile.id — Extend file ID for the completed PDF; reusable as input
    to other endpoints.
  - output.editedFile.presignedUrl — direct download URL for the completed PDF;
    expires 15 minutes after the run completes, so download or store it promptly.
  - output.filledValues — values written into the form, keyed by schema property
    name or detected field name.
  - metrics.processingTimeMs, pageCount, fieldCount, fieldsDetectedCount,
    fieldsAnnotatedCount, fieldDetectionTimeMs, fieldAnnotationTimeMs,
    fieldFillingTimeMs — present on processed runs when returned by the API.
  - status — PROCESSING, PROCESSED, or FAILED. On failure, inspect failureReason
    and failureMessage.

Use --output-file to auto-download output.editedFile. Without --output-file, the
filled PDF remains on Extend; use the returned file ID with ` + "`extend files download`" + `.`

const editSchemaGenerationDoc = `Generate Edit Schema detects PDF form fields and returns a schema you can pass to
` + "`extend edit --schema`" + `. The endpoint is synchronous: it returns the schema directly,
with no schema-generation run to poll or delete.

Response fields:
  - schema — final generated schema after optional mapping; pass this to edit.
  - annotatedSchema — original detected schema with field locations.
  - mappingResult — present when --input-schema is provided; includes matches,
    unmatchedInputPaths, and unusedFormFieldKeys.

Generation config:
  - instructions — guide field detection and naming.
  - inputSchema — existing edit schema to map onto detected fields.
  - advancedOptions.tableParsingEnabled — parse table regions as arrays of objects.
  - advancedOptions.radioEnumsEnabled — model radio groups as enums so only one
    widget in a group is selected.
  - advancedOptions.nativeFieldsOnly — only use native AcroForm fields and skip
    object detection.

A common production workflow is: generate a base schema, add values and any
root-level conditional requirements, then run ` + "`extend edit --schema`" + `.`
