package cli

import "strings"

// newParseOptionsTopicDoc registers `extend help parse-options` —
// reference documentation for the JSON shapes accepted by
// --chunk-strategy / --block-options / --advanced-options on
// `extend parse`. The body is a static snapshot of the schema as of
// API version 2026-02-09; canonical truth lives at
// https://docs.extend.ai/openapi.json (search ParseConfig).
//
// Each option entry follows the same shape:
//
//	NAME           type, default                — short noun phrase
//	  What:        plain-English behavior
//	  When:        scenario in which to flip from default
//	  Cost:        latency / response-size / dollar trade-off (if any)
//
// The goal is that an agent reading this can answer two questions
// without bouncing to external docs: "what does excelParsingMode
// mean?" and "should I set it for this task?".
func newParseOptionsTopicDoc() *CommandDoc {
	return &CommandDoc{
		Use:     "parse-options",
		Summary: "Decision guide for extend parse --chunk-strategy / --block-options / --advanced-options",
		Group:   "Help topics",
		Triggers: []string{
			"what fields go in parse block options",
			"parse advanced options reference",
			"chunking strategy options for extend parse",
			"extend parse json config shape",
			"extend parse blockoptions tables figures barcodes",
			"when should i enable agentic table or text processing",
			"what does excelparsingmode do",
		},
		RenderBody: renderParseOptionsTopicBody,
	}
}

func renderParseOptionsTopicBody(_ *CommandDoc) string {
	var b strings.Builder
	b.WriteString("Parse options\n\n")
	b.WriteString("Decision guide for the JSON shapes accepted by `extend parse`'s tuning\n")
	b.WriteString("flags. Each option lists what it does, when to flip it, and what the\n")
	b.WriteString("trade-off costs (latency, response size, or accuracy). Defaults are the\n")
	b.WriteString("server defaults; only include the keys you want to override.\n\n")
	b.WriteString("All three flag values accept inline JSON, a path, a `file://` URI, or\n")
	b.WriteString("`-` for stdin.\n\n")
	b.WriteString("Snapshot from API version 2026-02-09. For the latest field-by-field\n")
	b.WriteString("reference and any post-snapshot additions:\n\n")
	b.WriteString("  Web docs:  https://docs.extend.ai/developers/api-reference/endpoints/parse/create-parse-run\n")
	b.WriteString("  OpenAPI:   https://docs.extend.ai/openapi.json   (search for ParseConfig)\n\n")

	b.WriteString("Common scenarios → which option to set\n")
	b.WriteString("--------------------------------------\n\n")
	b.WriteString("  Multi-page tables sharing headers ........ --block-options '{\"tables\":{\"tableHeaderContinuationEnabled\":true}}'\n")
	b.WriteString("  Decode barcodes / QR codes ............... --block-options '{\"barcodes\":{\"readingEnabled\":true}}'\n")
	b.WriteString("  LaTeX out of math equations .............. --block-options '{\"formulas\":{\"enabled\":true}}'\n")
	b.WriteString("  Recover signatures in scans .............. --block-options '{\"text\":{\"signatureDetectionEnabled\":true}}'\n")
	b.WriteString("  Process only certain pages ............... --advanced-options '{\"pageRanges\":[{\"start\":1,\"end\":3}]}'\n")
	b.WriteString("  Word-level OCR with bounding boxes ....... --advanced-options '{\"returnOcr\":{\"words\":true}}'\n")
	b.WriteString("  Complex spreadsheets (merged cells) ...... --advanced-options '{\"excelParsingMode\":\"advanced\"}'\n")
	b.WriteString("  Raw numeric cells (not locale strings) ... --advanced-options '{\"excelUseRawCellValues\":true}'\n")
	b.WriteString("  .docx/.xlsx/.pptx with bounding boxes .... --advanced-options '{\"alwaysConvertToPdf\":true}'\n")
	b.WriteString("  Fast parse on simple typed PDFs .......... --engine parse_light\n")
	b.WriteString("  Detect tracked changes (redlines) ........ --advanced-options '{\"formattingDetection\":[{\"type\":\"change_tracking\"}]}'\n\n")

	b.WriteString("Top-level (already broken out as flags)\n")
	b.WriteString("---------------------------------------\n\n")
	b.WriteString("--target                  markdown | spatial   default: markdown\n")
	b.WriteString("  What: How to render text. `markdown` is reading-order text with\n")
	b.WriteString("        headings, lists, tables, checkboxes. `spatial` preserves\n")
	b.WriteString("        layout via whitespace; reads more like the original page.\n")
	b.WriteString("  When: `markdown` for LLMs/RAG and well-formed documents.\n")
	b.WriteString("        `spatial` for scans/handwriting/skewed pages or when exact\n")
	b.WriteString("        placement matters (BOLs, layout-heavy forms).\n")
	b.WriteString("  Note: `markdown` is required for --chunk-strategy section.\n\n")
	b.WriteString("--engine                  parse_performance | parse_light   default: parse_performance\n")
	b.WriteString("  What: Which parser to run. `performance` has full layout detection.\n")
	b.WriteString("        `light` is a faster, cheaper engine with no real layout\n")
	b.WriteString("        support and no `markdown` target.\n")
	b.WriteString("  When: `light` only when the doc is simple typed text and you want\n")
	b.WriteString("        speed/cost; otherwise default.\n\n")
	b.WriteString("--engine-version          string   default: latest\n")
	b.WriteString("  What: Pin a parser engine version for reproducibility.\n")
	b.WriteString("  When: Pin when you need stable outputs across releases. Some\n")
	b.WriteString("        advanced options require ≥ 2.0.0 — noted per-option below.\n\n")

	b.WriteString("--chunk-strategy / --chunk-min-chars / --chunk-max-chars\n")
	b.WriteString("--------------------------------------------------------\n\n")
	b.WriteString("Wire shape: { \"type\": \"page|document|section\", \"options\": { \"minCharacters\": int, \"maxCharacters\": int } }\n\n")
	b.WriteString("type        page | document | section   default: page\n")
	b.WriteString("  page      One chunk per page. Use when retrieval should be page-aware\n")
	b.WriteString("            (cite-by-page, page-bounded snippets).\n")
	b.WriteString("  document  Single chunk for the whole doc. Use for short docs or when\n")
	b.WriteString("            you want one big context window.\n")
	b.WriteString("  section   Split on logical sections (headings). Use for retrieval\n")
	b.WriteString("            over long structured docs. Requires --target markdown.\n\n")
	b.WriteString("options.minCharacters       int       default: 500\n")
	b.WriteString("options.maxCharacters       int       default: 10000\n")
	b.WriteString("  What: Bound chunk size for `section` chunking.\n")
	b.WriteString("  When: Increase max for fewer, larger chunks (cheaper retrieval,\n")
	b.WriteString("        less context per match). Decrease for finer-grained recall.\n\n")

	b.WriteString("--block-options (per-block-type detection)\n")
	b.WriteString("------------------------------------------\n\n")
	b.WriteString("Top-level keys: figures, tables, text, keyValue, barcodes, formulas.\n")
	b.WriteString("All optional; omit a key to keep its defaults.\n\n")

	b.WriteString("figures\n")
	b.WriteString("  enabled                          bool   default: true\n")
	b.WriteString("    What: Whether figure blocks are emitted at all.\n")
	b.WriteString("    When: Disable only when you don't need figures and want a\n")
	b.WriteString("          smaller response.\n")
	b.WriteString("  figureImageClippingEnabled       bool   default: true\n")
	b.WriteString("    What: Clip each figure as a separate image (returned in `imageUrl`).\n")
	b.WriteString("    When: Disable to skip image extraction and reduce processing.\n")
	b.WriteString("  advancedChartExtractionEnabled   bool   default: false\n")
	b.WriteString("    What: Use a vision model to extract structured data from charts\n")
	b.WriteString("          (axis values, bar heights, legends).\n")
	b.WriteString("    When: Enable when charts contain data you actually want to\n")
	b.WriteString("          recover; default off because most parses don't need it.\n")
	b.WriteString("    Cost: Extra VLM call per chart; meaningful latency increase.\n\n")

	b.WriteString("tables\n")
	b.WriteString("  targetFormat                     markdown | html   default: html\n")
	b.WriteString("    What: Format of the `content` string on each table block.\n")
	b.WriteString("    When: `html` preserves complex layouts (merged cells, spans).\n")
	b.WriteString("          `markdown` is more LLM-friendly for simple tables.\n")
	b.WriteString("  tableHeaderContinuationEnabled   bool   default: false\n")
	b.WriteString("    What: Copy column headers from a first-page table onto headerless\n")
	b.WriteString("          continuation tables on later pages, when column counts match.\n")
	b.WriteString("    When: Enable for multi-page tables that don't repeat headers\n")
	b.WriteString("          (statements, invoices, journals).\n")
	b.WriteString("  cellBlocksEnabled                bool   default: false\n")
	b.WriteString("    What: Emit each cell as its own child block (bbox + content).\n")
	b.WriteString("    When: Enable when you need per-cell coordinates or downstream\n")
	b.WriteString("          consumers expect a flat block stream.\n")
	b.WriteString("    Cost: Bigger response; more blocks to iterate.\n")
	b.WriteString("  agentic.enabled                  bool   default: false\n")
	b.WriteString("    What: Run a VLM review/correction pass over detected tables.\n")
	b.WriteString("    When: Enable on high-stakes tables with tricky layouts or noisy\n")
	b.WriteString("          OCR. Often the right knob when default tables look wrong.\n")
	b.WriteString("    Cost: Extra VLM call per table; significant latency + dollars.\n")
	b.WriteString("  agentic.customInstructions       string\n")
	b.WriteString("    What: Prose to steer the VLM pass.\n")
	b.WriteString("    When: When you know the domain quirks (e.g. \"treat blank cells\n")
	b.WriteString("          as zeros\", \"currency column is right-aligned but headers are left\").\n\n")

	b.WriteString("text\n")
	b.WriteString("  signatureDetectionEnabled        bool   default: false\n")
	b.WriteString("    What: Extra vision model that detects signatures (otherwise they\n")
	b.WriteString("          appear as garbled text blocks).\n")
	b.WriteString("    When: Enable for contracts, agreements, notarized forms.\n")
	b.WriteString("    Cost: Adds a vision pass; some latency.\n")
	b.WriteString("  agentic.enabled                  bool   default: false\n")
	b.WriteString("    What: VLM pass that corrects OCR errors in text blocks.\n")
	b.WriteString("    When: Enable for scans, handwriting, or low-quality input where\n")
	b.WriteString("          default OCR is wrong on specific tokens.\n")
	b.WriteString("    Cost: VLM call per page; latency + dollars.\n")
	b.WriteString("  agentic.customInstructions       string\n")
	b.WriteString("    What: Prose guidance for OCR corrections.\n")
	b.WriteString("    When: When you know failure modes ahead of time (e.g. \"do not\n")
	b.WriteString("          collapse multiple spaces\", \"preserve all-caps\").\n\n")

	b.WriteString("keyValue\n")
	b.WriteString("  blankFieldFormattingEnabled      bool   default: false\n")
	b.WriteString("    What: Preserve formatting for KV pairs whose value is blank\n")
	b.WriteString("          (otherwise blank-value pairs are collapsed).\n")
	b.WriteString("    When: Enable when you care about which form fields are blank\n")
	b.WriteString("          vs absent (form-completion audits, compliance review).\n\n")

	b.WriteString("barcodes\n")
	b.WriteString("  imageClippingEnabled             bool   default: false\n")
	b.WriteString("    What: Clip each barcode as an image (returned in `imageUrl`).\n")
	b.WriteString("    When: Enable if downstream needs the barcode pixels (e.g. you\n")
	b.WriteString("          want to re-decode or display them).\n")
	b.WriteString("  readingEnabled                   bool   default: false\n")
	b.WriteString("    What: Decode the barcode/QR value (returned in `decodedValue`).\n")
	b.WriteString("    When: Enable to actually read barcodes — the parser does NOT\n")
	b.WriteString("          decode them by default; you only get the location/type.\n\n")

	b.WriteString("formulas\n")
	b.WriteString("  enabled                          bool   default: false\n")
	b.WriteString("    What: Detect math equations and return LaTeX in block details.\n")
	b.WriteString("    When: Enable for academic/technical/scientific PDFs. With this\n")
	b.WriteString("          off, equations are treated as text and usually garbled.\n\n")

	b.WriteString("--advanced-options (engine-level tuning)\n")
	b.WriteString("----------------------------------------\n\n")
	b.WriteString("Document-wide settings that apply across the whole parse.\n\n")

	b.WriteString("Page selection\n")
	b.WriteString("  pageRotationEnabled        bool   default: true\n")
	b.WriteString("    What: Auto-detect and correct rotated pages (90°/180°/270°).\n")
	b.WriteString("    When: Leave on unless you've pre-corrected input. Disabling\n")
	b.WriteString("          saves a tiny amount of latency and is rarely worth it.\n")
	b.WriteString("  pageRanges                 [ {start:int, end:int}, ... ]\n")
	b.WriteString("    What: Process only the listed (inclusive) page ranges.\n")
	b.WriteString("    When: Use on large PDFs where you only need specific pages.\n")
	b.WriteString("    Cost: Saves linear-in-pages processing time and dollars.\n\n")

	b.WriteString("Excel parsing (.xlsx only; .xls is always basic)\n")
	b.WriteString("  excelParsingMode           basic | advanced\n")
	b.WriteString("    What: Strategy for parsing Excel workbooks.\n")
	b.WriteString("          `basic`    — fast, deterministic, cell-by-cell read.\n")
	b.WriteString("                       Treats sheets as flat data tables.\n")
	b.WriteString("          `advanced` — layout-block detection (merged cells,\n")
	b.WriteString("                       multiple side-by-side tables, headers).\n")
	b.WriteString("    When: `advanced` for spreadsheets that look like printed reports\n")
	b.WriteString("          rather than raw data; otherwise `basic`.\n")
	b.WriteString("    Cost: `advanced` is slower; pick deliberately.\n")
	b.WriteString("  excelSkipHiddenContent     bool   default: false\n")
	b.WriteString("    What: Skip hidden rows, columns, and sheets.\n")
	b.WriteString("    When: Enable when hidden content is scratch/lookup data and\n")
	b.WriteString("          shouldn't surface in the output.\n")
	b.WriteString("  excelUseRawCellValues      bool   default: false\n")
	b.WriteString("    What: Return raw cell values rather than locale-formatted\n")
	b.WriteString("          strings — `12345.67` instead of `\"$12,345.67\"`.\n")
	b.WriteString("    When: Enable when downstream needs numbers for math, not\n")
	b.WriteString("          display strings.\n")
	b.WriteString("  excelSkipCalculation       bool   default: true\n")
	b.WriteString("    What: Skip formula recalculation on workbook open.\n")
	b.WriteString("    When: Disable only if cell values depend on volatile functions\n")
	b.WriteString("          like NOW() or TODAY() that need fresh values.\n")
	b.WriteString("    Cost: Recalculation is a major latency hit on big workbooks —\n")
	b.WriteString("          keep the default unless you know you need it.\n\n")

	b.WriteString("Image / Office conversion\n")
	b.WriteString("  alwaysConvertToPdf         bool   default: false\n")
	b.WriteString("    What: Convert images, .docx, .pptx, .xlsx, .html to PDF before\n")
	b.WriteString("          parsing. PDF backing gives you reliable bounding boxes.\n")
	b.WriteString("    When: Enable for non-PDF inputs when you need bbox coordinates\n")
	b.WriteString("          or spatial output. Default off because the conversion\n")
	b.WriteString("          adds latency.\n")
	b.WriteString("  imageConversionQuality     high | medium | low   default: medium\n")
	b.WriteString("    What: Quality used when converting to PDF (above).\n")
	b.WriteString("    When: `high` for dense or large docs where conversion loss matters;\n")
	b.WriteString("          `low` when you want speed/size and fidelity is fine to drop.\n\n")

	b.WriteString("Spatial-only tuning\n")
	b.WriteString("  verticalGroupingThreshold  number 0.1-5.0   default: 1.0\n")
	b.WriteString("    What: Multiplier for the Y-axis distance that decides whether two\n")
	b.WriteString("          text blocks are \"on the same line\". Higher values group\n")
	b.WriteString("          blocks that are further apart vertically.\n")
	b.WriteString("    When: Tweak only when --target spatial output is splitting or\n")
	b.WriteString("          merging lines incorrectly. Default is fine for most docs.\n")
	b.WriteString("    Note: No effect unless --target spatial.\n\n")

	b.WriteString("Output enrichment\n")
	b.WriteString("  returnOcr.words            bool   default: false\n")
	b.WriteString("    What: Include word-level OCR data (text, per-word bbox, confidence).\n")
	b.WriteString("    When: Enable for highlight/redaction, validation, or layout-aware\n")
	b.WriteString("          downstream processing.\n")
	b.WriteString("    Cost: Massively inflates response size — prefer the signed-URL\n")
	b.WriteString("          output return format for large jobs.\n")
	b.WriteString("  enrichmentFormat           xml | bracket   default: xml\n")
	b.WriteString("    What: Format of inline annotations in `content`.\n")
	b.WriteString("          `xml`     — <page_number>1</page_number>, <barcode>...</barcode>\n")
	b.WriteString("          `bracket` — [page_number: 1], [barcode: ...]\n")
	b.WriteString("    When: Pick whichever your downstream parses more easily.\n")
	b.WriteString("  formattingDetection        [ { \"type\": \"change_tracking\" } ]\n")
	b.WriteString("    What: Detect formatting-based annotations. Currently only\n")
	b.WriteString("          `change_tracking` — recover insertions/deletions/substitutions\n")
	b.WriteString("          indicated by strikethroughs, colored text, or underlines.\n")
	b.WriteString("    When: Enable for redlined contracts, edited drafts, anywhere you\n")
	b.WriteString("          need to recover tracked changes.\n")
	b.WriteString("    Note: Requires --engine parse_performance and --engine-version ≥ 2.0.0.\n")
	b.WriteString("          Affected block types: text, heading, section_heading, header, footer.\n\n")

	b.WriteString("Gotchas\n")
	b.WriteString("-------\n\n")
	b.WriteString("- --target spatial does not support --chunk-strategy section.\n")
	b.WriteString("- blockOptions.tables.enabled is deprecated and has no effect; use\n")
	b.WriteString("  the individual feature flags instead.\n")
	b.WriteString("- returnOcr.words can make responses huge; prefer the signed-URL\n")
	b.WriteString("  output return format for large jobs.\n")
	b.WriteString("- formattingDetection requires engine=parse_performance >= 2.0.0.\n")
	b.WriteString("- excelSkipCalculation=false makes parsing dramatically slower on\n")
	b.WriteString("  large/formula-heavy workbooks. Only disable if you actually need\n")
	b.WriteString("  fresh values from volatile functions.\n")
	return b.String()
}
