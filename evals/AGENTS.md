# Skill-eval authoring contract

These evals measure whether real coding-agent harnesses (Claude Code,
Codex) make the right decisions when our `extend` skill is loaded. Every
test case in `evals.json` is a structured claim about the SKILL.md: if
the skill genuinely communicates "use `extend extract` for typed-field
extraction" then a representative agent prompt should produce an
`extend extract` invocation. The runner collects evidence; the
expectations declare what evidence counts.

## Two prompt styles, picked deliberately per case

Every case is either **Path A** (natural, incomplete prompt — tests
agent judgement) or **Path B** (explicit context — tests command shape).

| Path | Used for | Prompt style | Assertion shape |
|---|---|---|---|
| **A** | `T+*`, `T-*`, `D-*`, `A-*`, `F-3` | User-style natural language; no IDs/configs | verb correctness + missing-context handling + ID-fabrication ban |
| **B** | `S-*`, `F-1`/`F-2`, `W-*`, `Q-*`, `P-*`, `AP-*`, `H-*`, `E-*` | Provides every ID and file the agent needs | exact-shape mechanical assertions |

Path A prompts deliberately omit IDs an agent could not know. The
correct behaviour is multi-step: ask, run a `list`, or use an inline
config. Asserting `--using ex_abc` on a Path-A case is a bug — the
agent had no way to know that ID. Path B prompts carry every ID;
"agent emits exactly this argv" is fair game.

## Expectation types

`evals.json` evals carry an `expectations` array. Each element is one of:

- **`skill_activates`** — records that the agent reached for the skill
  (read `SKILL.md`, invoked the stub, or both). Used by trigger-positive
  cases to confirm the skill activated at all.
- **`extend_call`** — mechanical. Walks `extend-calls.jsonl` for argv
  predicates: `must_contain`, `must_not_contain`, `count_under`,
  `count_at_least`. Both flags and positional args are matchable.
- **`stable_answer`** — deterministic match against the agent's final
  message. Substring or regex. Cheap; no LLM judge.
- **`must_not_fabricate_ids`** — scans every recorded `extend` call for
  ID-shaped tokens (`ex_*`, `cl_*`, `spl_*`, `workflow_*`, `evs_*`,
  `webhook_*`, `webhook_subscription_*`, etc.) and reports any that did
  *not* appear in a prior stub response. Catches the most common
  Path-A failure mode (the agent making up an ID rather than asking).
- **`judge`** — LLM-judge, for prose-quality questions ("did the
  explanation acknowledge the run-type quirk correctly?"). Runs unless
  disabled with `-no-judge` (a cheap, judge-free pre-PR sweep).

Most cases use ≥2 types in combination. Stable-answer +
`must_not_fabricate_ids` together cover the majority of Path-A
expectations cheaply (no model calls).

## Discriminating-assertion rule

Every expectation must satisfy this test before it lands:

> Could this expectation pass for an output the user would consider
> clearly wrong?

If yes, the expectation is too lenient and needs tightening. Examples:

- `extend_call must_contain extract` alone is too lenient — passes if
  the agent ran `extend extract` against the wrong file or with
  fabricated flags. Tighten with positional-arg matching or a partner
  `must_not_contain` predicate.
- `judge: "agent explained the auth flow"` is too lenient — passes for
  any answer that mentions "auth". Re-phrase to a falsifiable claim:
  `"Mentions either EXTEND_API_KEY or --workspace and does not invent
  an `extend auth login` command."`
- `stable_answer: "extract"` is far too lenient — matches any text
  containing the word. Use `--using` or `extend extract ` (with the
  trailing space) to anchor the match.

This is enforced by author discipline, not the runner. Reviewers should
push back on any expectation that fails the discriminating test.

## Stub modes

The fake `extend` binary (`evals/stub`) operates in modes set per case
via `stub_config.default_mode`:

- **`real_responses`** (default) — small fixture set of extractors,
  classifiers, etc. List/get calls return canned realistic data; run
  calls return canned terminal-state results. The IDs in fixtures are
  the *only* IDs an agent can legitimately use without fabrication.
- **`paginated`** — list calls return pages with `nextPageToken`. Used
  for `P-*` cases that test pagination discipline. Configurable page
  count.
- **`auth_error`** — every call returns a 401 with the actual auth
  error envelope. Used for `E-*` cases that test recovery.

Modes are layered: a `paginated` case can still get the standard
fixture set on get/upload calls.

## Help-discovery cases and stub help

`H-*` (help-discovery) cases assert that an agent can find a flag or
capability by consulting `extend <cmd> --help` rather than guessing. The
fake binary serves per-command help from the `commandHelp` map in
`evals/stub` — a hand-maintained mirror of the real CLI's flags, scoped
per command the way `extend <cmd> --help` is. A help-discovery case is
only meaningful if that mirror carries the flag under test:

- When you add an `H-*` case for a flag, confirm `evals/stub`'s
  `commandHelp[<command>]` actually lists it (the case will otherwise
  pass or fail for the wrong reason).
- Keep `commandHelp` in sync with the real command docs in
  `internal/cli/`. The integration tests (e.g. `TestEditAdvancedOptions_ExposedInBinary`)
  guard that the *real* `--help` carries a flag; the stub mirror is what
  the agent sees in an eval. Drift between the two silently weakens the
  signal.

## ID-fabrication checking

The `must_not_fabricate_ids` checker tracks which IDs the stub
*returned* across the run (parsing stdout JSON of all prior calls) and
flags any matching-pattern ID in a subsequent call's argv that did not
come from those returns. Adding a new ID-prefixed entity to the CLI
means adding the prefix pattern to the default `patterns` list in
`grade/fabrication.go`.

## Adding a case

1. Decide Path A or B; write the prompt accordingly.
2. List inputs in `files`. Stage them in `evals/files/`.
3. Write expectations in priority order (most-specific first).
4. Run the discriminating-assertion test on each expectation.
5. Run the case against at least one harness and iterate until the
   expectations stabilise around the agent's actual behaviour rather than
   your imagined behaviour:

       cd evals/runner && go run ./cmd/runner -cases <ID> -harnesses claude_code -runs 1

   See `evals/README.md` for harness auth and runner flags.

Cases that fail the same way against both harnesses are usually a
SKILL.md problem, not an eval problem; that's the signal we're after.
