# Skill evals

This directory measures how well our `extend` skill makes real coding
agents do the right thing. Each test case is a structured claim about
the skill: when a user asks a representative thing, do Claude Code and
Codex pick the right CLI commands?

The runner spawns each harness with the skill installed (`with_skill`)
and without it (`without_skill`), records every `extend` invocation
the agent makes against a fake recording binary, then grades the
evidence against per-case expectations.

## Quick start

```sh
# Install the harnesses (once).
mise install                       # provisions claude + codex via mise.toml

# Make sure each harness is authenticated.
claude setup-token                 # or set ANTHROPIC_API_KEY
codex login                        # ChatGPT login or set OPENAI_API_KEY

# Run the full eval set against both harnesses.
cd evals/runner
go run ./cmd/runner

# Run a subset for fast iteration.
go run ./cmd/runner -cases T+1,S-1 -harnesses claude_code -runs 1
```

Per-run artifacts land in `extend-cli-evals-workspace/iteration-N/`
(sibling of the repo, gitignored). The runner prints a benchmark
summary at the end and writes `iteration-N/benchmark.json` for trend
analysis.

## How it stays deterministic

The agent's `extend` invocations hit a stub binary (`evals/stub`) that
returns canned realistic responses without contacting the real Extend
API. Three modes:

- **`real_responses`** (default): list/get/upload return canned
  fixtures; action verbs return realistic terminal-state runs.
- **`paginated`**: list calls return pages with `nextPageToken` so
  pagination-discipline cases have multiple pages.
- **`auth_error`**: every call exits with a 401 envelope for
  error-recovery cases.

Mode is selected per case via `stub_config.default_mode` in
`evals.json`.

## Adding a case

See `evals/AGENTS.md` for the full authoring contract. Short version:

1. Decide Path A (natural prompt; tests judgement) or Path B (explicit
   context; tests command shape).
2. Add an entry to `evals.json` with `id`, `category`, `path`,
   `prompt`, `files`, `modes`, and `expectations`.
3. Stage any new fixture inputs in `runner/fixtures/fixtures.go`.
   Fixtures are generated on demand at run time, not committed.
4. Run the case once locally: `go run ./cmd/runner -cases <ID>
   -harnesses claude_code -runs 1`.
5. Iterate on the expectations until they match the agent's actual
   correct behaviour, not your imagined behaviour.

## Cost

A single 4-case sweep against both harnesses with `-runs 1` typically
consumes:

- Claude Code: ~3,000–4,000 tokens per case-config (with cache hits)
- Codex: ~100,000–200,000 tokens per case-config (full skill loaded
  into reasoning context every turn)

Bump `-runs 3` for stddev estimation; cost scales linearly. Use
`-cases X,Y,Z` and `-harnesses claude_code` to scope down during
development.


