# extend-cli

> [!WARNING]
> This CLI is under active development. The command surface, flags, and
> output formats are not yet stable and may change without notice. Pin to
> a specific tag if you depend on a particular shape of output.

A CLI for [Extend](https://extend.ai).

## Install

Install script (macOS, Linux):

    curl -fsSL https://extend.ai/install.sh | sh

Homebrew (macOS, Linux):

    brew install extend-hq/tap/extend

npm / npx (no install):

    npx @extend-ai/cli --help
    # or globally
    npm install -g @extend-ai/cli

From source (requires Go toolchain):

    go install github.com/extend-hq/extend-cli/cmd/extend@latest

Or grab a signed binary from the [releases page](https://github.com/extend-hq/extend-cli/releases).
The install script verifies the release checksum and installs to a directory
already on your `PATH`. See its options with:

    curl -fsSL https://extend.ai/install.sh | sh -s -- --help

## Setup

    extend setup

The wizard asks how you want to sign in — through your browser (no API
key needed) or with an API key — then picks your region, walks you
through the rest, and installs the agent skill. See
[Authentication](#authentication) for how the two credential types
differ.

## Authentication

**Browser login** (`extend setup`, or `extend login` directly) is best
on your own machine: nothing to create or copy, tokens are stored
securely and refresh on their own, and the session is scoped to the
workspace and environment you approve. `extend logout` revokes it;
`extend whoami` shows who you're signed in as.

**API keys** are long-lived and headless — the right choice for
scripts, CI, and agents. Save one with `extend setup`, or set
environment variables directly:

    export EXTEND_API_KEY=sk_xxx
    export EXTEND_REGION=us                        # us | us2 | eu (default: us)
    export EXTEND_WORKSPACE_ID=ws_xxx              # for org-scoped keys

When several sources are configured, credentials are used in this
order:

1. `EXTEND_API_KEY` (or `EXTEND_<LABEL>_API_KEY` under `--env <label>`)
2. The API key saved by `extend setup`
3. The stored `extend login` session

Check what is in effect with `extend whoami` or `extend config`. For
token storage details and troubleshooting, run `extend help auth`.

## Use with coding agents

The CLI ships a [`SKILL.md`](https://agentskills.io) that teaches
agent harnesses (Claude Code, Codex, OpenCode, Cursor, Goose, etc.)
to use `extend` correctly without you having to spell out every command.

Install to the cross-client default path:

    extend skill install

This writes `~/.agents/skills/extend-cli/SKILL.md`, the path Codex,
OpenCode, Cursor, and most other harnesses read, and symlinks it into
`~/.claude/skills/extend-cli` for Claude Code. To scope the skill to
one project instead of your whole machine:

    extend skill install --target ./.agents/skills/extend-cli/SKILL.md

Re-run `extend skill install` after upgrading to pick up new commands
and flag changes.

## Examples

    # extract structured data from a local PDF
    extend extract invoice.pdf --using ex_abc

    # parse to markdown
    extend parse contract.pdf > contract.md

    # run a workflow async; poll later
    RUN=$(extend run doc.pdf --using workflow_abc -o id)
    extend runs watch "$RUN"

    # filter JSON with jq
    extend extract invoice.pdf --using ex_abc --jq '.output.value.invoice_id' -o raw

    # batch
    extend extract batch invoices/*.pdf --using ex_abc

Inputs can be a local path (auto-uploads), a `file_xxx` ID, or an
`https://` URL.

## Commands

    extract | classify | split | run  <input> --using <id>
    parse <input>
    edit <input> --schema schema.json
    <action> batch <inputs>... [--files-from list.txt]

    runs    get | list | watch | cancel | delete | update
    batches get | watch
    files   upload | list | get | delete | download

    extractors  | classifiers | splitters | workflows
        list | get | create | update | versions ...

    evaluations         list | get | create
    evaluations items   list | get | create | update | delete
    evaluations runs    get

    webhooks endpoints     | subscriptions   list | get | create | update | delete
    webhooks verify

Run `extend <command> --help` for flags.

## Output

`-o json|yaml|raw|id|table|markdown` overrides the per-command default.
`--jq '<expr>'` filters structured payloads before `json`, `yaml`, `raw`, or
`id` formatting. Data goes to stdout, status to stderr. Honors `NO_COLOR` and
`CLICOLOR_FORCE`.
