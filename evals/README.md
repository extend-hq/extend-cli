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

# Set ANTHROPIC_API_KEY for Claude Code and the judge; the isolated
# Claude home does not reuse a saved login.
# Codex reuses its saved login, or accepts CODEX_API_KEY without one.
codex login                        # optional if CODEX_API_KEY is set

# Run the full eval set against both harnesses.
cd evals/runner
go run ./cmd/runner

# Run a subset for fast iteration.
go run ./cmd/runner -cases T+1,S-1 -harnesses claude_code -runs 1

# Compare two Claude models head-to-head against Codex. Each -claude-models
# entry runs as its own harness named claude_code:<model>; -harnesses still
# filters by family ("claude_code" selects every pinned Claude variant).
# -codex-models does the same for Codex.
go run ./cmd/runner -claude-models claude-opus-5-5,claude-sonnet-5-5
```

By default each harness runs the way a fresh install does: on its own
default model and effort, which change as the harnesses release. Pin
models with `-claude-models` / `-codex-models` and set one effort for
every harness with `-effort` (e.g. `-effort low` for a cheaper sweep).
The run prints each harness's version up front, and the summary and
`benchmark.json` record the models each harness reported (Codex
doesn't report its default model, so only its version is recorded).
`judge` expectations are graded by `claude-opus-5-5` (`-judge-model`).

The summary also prints a per-harness table so the model-vs-model
delta is visible at a glance:

```
=== Per-harness ===
harness                              with_skill  without_skill        delta
claude_code:claude-opus-5-5              94.2%          57.1%       +37.1pp
claude_code:claude-sonnet-5-5             96.5%          59.0%       +37.5pp
codex                                    91.0%          55.0%       +36.0pp
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

A single 4-case sweep against both harnesses with `-runs 1` used to
consume the amounts below, measured at `-effort low` on older models;
the harness defaults usually think more:

- Claude Code: ~3,000–4,000 tokens per case-config (with cache hits)
- Codex: ~100,000–200,000 tokens per case-config (full skill loaded
  into reasoning context every turn)

Bump `-runs 3` for stddev estimation; cost scales linearly. Use
`-cases X,Y,Z` and `-harnesses claude_code` to scope down during
development.

