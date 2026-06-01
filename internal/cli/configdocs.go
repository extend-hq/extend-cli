package cli

// Config field catalogs shared between the action verbs (the `--config`
// object on extract/classify/split) and the create/update request bodies
// (extractors/classifiers/splitters/workflows `--from-file`).
//
// Per the documentation contract in internal/cli/AGENTS.md ("Flags vs.
// JSON config"): config objects ride in a single JSON flag rather than
// per-field flags, and discoverability is satisfied by documenting the
// field catalog in Details. These constants are that catalog.
//
// Source of truth is the pinned extend-go-sdk config structs:
// SplitConfig, ExtractConfigJSON, ClassifyConfig, and
// WorkflowsCreateRequest.Steps. When the SDK is regenerated and a config
// field is added, removed, or renamed, update the matching catalog here
// in the same change — drift is a defect, not a follow-up.

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
