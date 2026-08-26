# Documentation contract

Documentation is a primary artifact of this package, not a side product.
Every command, group, and help topic is a `*CommandDoc` literal in the
tree rooted at `RootDoc(app)`. The cobra command tree built by
`RootDoc(app).Build()` is one of N projections of that tree; downstream
consumers (the help renderer, the `extend skill` SKILL.md generator, and
planned MCP tool manifests) read from the typed tree via `Walk(RootDoc(app))`,
never from cobra annotations.

## Three node kinds

A `*CommandDoc` is exactly one of:

| Kind | RunE | RenderBody | Subcommands |
|---|---|---|---|
| **Command leaf** | required | nil | optional (a leaf may have children, e.g. `extract` + `extract batch`) |
| **Group** | nil | nil | required (≥1) |
| **Help topic** | nil (Build wires it) | required | none |

`Validate()` rejects ambiguous docs at `Build()` time with a panic; tests
catch this before the binary is shipped.

## Required fields per kind

| Field | Command leaf | Group | Topic |
|---|---|---|---|
| `Use`, `Summary` | required | required | required |
| `Triggers` (≥3) | required | optional | required |
| `WhenToUse` or `Details` | one or both | one or both | n/a (body is dynamic) |
| `Examples` (≥1) | required | n/a | n/a |
| `Output` | required (TTY+Pipe) | n/a | n/a |
| `Wait` ↔ `Failures` | optional pair | n/a | n/a |
| `RenderBody` | must be nil | must be nil | required |
| `RunE` | required | must be nil | must be nil |
| `Group` | optional ("Actions"/"Inspection"/"Resources") | optional | required ("Help topics") |

## Field-level rules

**`Summary`** — 10–80 characters, starts with a capital, no trailing period.
Becomes cobra's `Short` and the first line of the SKILL.md catalog entry
for this command. Keep it action-oriented and specific.

**`Triggers`** — at least 3 entries, each ≥10 characters, all lowercase, no
duplicates with siblings (validated cross-doc). These are the phrases an
agent matches against when deciding whether to invoke this command. Cover
multiple phrasings of the same intent:

- the verb form ("extract structured data from a document")
- common synonyms ("pull fields from an invoice or receipt")
- the noun-object combination ("schema-driven document extraction")
- a likely LLM rephrasing ("ocr a contract with a defined schema")
- a literal-tool-name match ("run an extractor against a pdf")

**`WhenToUse`** — selection guidance: "Use this when X; prefer Y for Z".
Concise prose, addressed to the user. The single highest-leverage field
for agent quality. Compare to sibling commands explicitly when relevant.

**`Details`** — the reference body. Free-form prose; can include code
blocks and tables. Don't repeat what `WhenToUse` says.

**`Examples`** — ≥1 entry, each with a `Label` (section header) and a
`Cmd` that invokes "extend ". `Label` is what agents and humans skim; keep
it short ("Basic", "URL input", "Async", "With jq filter"). The `Cmd`
must contain "extend " somewhere — this allows pipeline forms like
`ls *.pdf | extend extract batch ...`.

**`Gotchas`** — non-obvious behaviours and common mistakes. Each entry is
a complete sentence ending in a period. The corrections an agent needs to
hear *once* to avoid a class of error. Examples: "Page tokens are bound
to the originating query; repeat the same filter flags on every paginated
call." or "Workflow batches do not have a get endpoint; use 'extend
workflows runs list --batch <id>' instead."

**`SeeAlso`** — related command paths in space-separated form
("runs watch", "extract"), validated cross-doc. Optional.

**`Output`** — required for command leaves. `TTY` is the default when
stdout is a terminal; `Pipe` is the default when piped. Both must be
members of `OutputModes`.

**`Wait` and `Failures`** — set together. `Wait` describes the polling
profile and default behaviour; `Failures` lists the run statuses that
gate non-zero exit. Validators reject `Wait` set with empty `Failures`
or vice versa.

**`Args`** — optional `cobra.PositionalArgs`. Defaults to `cobra.NoArgs`
for groups when nil.

**`Configure`** — runs after annotation application; bind flags, set
hooks here. The closure captures local-var pointers shared with `RunE`.

## Flags vs. JSON config

When a command needs to set fields of an API **config object**
(`config`, `advancedOptions`, `blockOptions`, processor config, an
edit/extract/classify/split config), take the whole object as a single
JSON flag — `--config` / `--advanced-options` / `--block-options` /
`--from-file` (inline JSON, a path, a `file://` URI, or `-`) — and
unmarshal it into the SDK struct. Do **not** mint one flag per field.

Rationale: per-field flags are high-maintenance (every new API field
needs a flag + wiring + a test) and can't represent nested objects or
arrays (e.g. parse's `pageRanges`, `returnOcr`); a JSON flag is one
unmarshal and absorbs the whole shape. It's also the consistent shape
across the action verbs (`extract`/`classify`/`split` configs, parse's
deep options, edit's `advancedOptions`).

Discoverability — the reason these knobs existed-but-were-invisible in
the first place — is satisfied by **documenting the field catalog in
`Details`** (field name, type, one-line meaning, default), not by
promoting fields to flags. The agent reads `extend <cmd> --help`, sees
the JSON shape, and builds it.

Reserve typed flags for: run/IO controls (`--wait`, `--output-file`,
`--using`, `--version`, `-o/--output`) and a small set of genuinely
common, flat, stable knobs a command wants to promote (parse's
`--target`/`--chunk-strategy`/`--engine`). Everything else in a config
object goes through the JSON flag.

## Adding a new command

1. Write `func newFooDoc(app *App) *CommandDoc` returning a literal.
2. Add it to the appropriate parent's `Subcommands` list.
3. `go test ./internal/cli/...` — strict validators surface every
   missing or malformed field.
4. `go run ./cmd/extend foo --help` — eyeball the rendered output;
   reorder `Examples` if the most common case isn't first.

## When the contract changes

Drift is a build break, not a warning. Every contract change in this
file should land alongside the corresponding test change in
`commanddoc_test.go`. Don't loosen a rule to make code pass; either
fix the code or get explicit agreement to weaken the rule first.
