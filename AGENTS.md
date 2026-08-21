# AGENTS.md

`extend` is a Go CLI for the Extend document AI platform. Entrypoint:
`cmd/extend`. Every command is built on the Extend Go SDK
(`github.com/extend-hq/extend-go-sdk`) wired through `internal/extendx`.

## Build & test

    go build ./...
    go test ./...        # unit tests; no network, fast
    go vet ./...
    gofmt -l cmd internal # must print nothing

Integration tests are a separate module under `test/integration` (black-box:
they build and exec the binary). They need `EXTEND_BASE_URL` and
`EXTEND_API_KEY` and skip silently without them; run-creating ops that spend
credits are gated behind `EXTEND_TEST_RUN_OPS=1`:

    cd test/integration && go test ./...

Skill evals (drive real Claude Code / Codex harnesses) are another module
under `evals/runner`; see `evals/README.md`.

## Layout

- `cmd/extend` — main entrypoint.
- `internal/cli` — every command, group, and help topic; the typed
  CommandDoc tree is the source of truth (see `internal/cli/AGENTS.md`).
- `internal/extendx` — SDK client construction, run polling/wait, file I/O.
- `internal/oauth`: browser login (PKCE flow, loopback redirect, token
  storage in the keychain with file fallback, silent refresh).
- `internal/output`, `internal/iostreams` — rendering and stream handling.
- `evals/` — skill-eval harness (see `evals/AGENTS.md`).

## Conventions

- A command's `--help` and the generated `SKILL.md` are projections of the
  CommandDoc tree — change the doc, never hand-edit generated skill output.
- Never commit secrets or API keys.

## Keep the scoped AGENTS.md files current

Subtrees with their own contracts carry a local `AGENTS.md` that is the
source of truth for their area. When a change touches that area, update its
`AGENTS.md` in the **same change** — drift is a defect, not a follow-up:

- `internal/cli/AGENTS.md` — the CommandDoc documentation contract: how
  every command, group, and help topic is declared, the required fields per
  node kind, and the validators that enforce them.
- `evals/AGENTS.md` — the skill-eval authoring contract: the case schema and
  Path A/B split, expectation types, stub modes and per-command help, and
  ID-fabrication patterns.

When you add a new `AGENTS.md`, add it to this list.
