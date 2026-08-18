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

## Use with coding agents

The CLI ships a [`SKILL.md`](https://agentskills.io) that teaches
agent harnesses (Claude Code, Codex, OpenCode, Cursor, Goose, etc.)
to use `extend` correctly without you having to spell out every command.

Install to the cross-client default path:

    extend skill install

This writes `~/.agents/skills/extend/SKILL.md`, the path Codex,
OpenCode, Cursor, and most other harnesses look at. Claude Code reads
from `~/.claude/skills/` instead, so point `--target` at it:

    extend skill install --target ~/.claude/skills/extend/SKILL.md

Make sure to re-run
`extend skill install` after upgrading to pick up new commands and
flag changes.

## Authenticate

Sign in through your browser (no API key needed):

    extend login

This runs an OAuth flow: your browser opens Extend's consent screen, you
pick one workspace and one environment, and the CLI stores the resulting
tokens in the OS keychain (or a 0600 file under `~/.config/extend/` on
headless hosts). Tokens refresh silently; `extend logout` revokes the
session and clears them.

Or run the interactive wizard; it picks your region, validates the API key,
and saves it to `~/.config/extend/config.json`:

    extend setup

Or set environment variables:

    export EXTEND_API_KEY=sk_xxx

Optional:

    export EXTEND_REGION=us                        # us | us2 | eu (default: us)
    export EXTEND_WORKSPACE_ID=ws_xxx              # for org-scoped keys

Precedence when several sources are configured: `EXTEND_API_KEY` (or
`EXTEND_<LABEL>_API_KEY` under `--env <label>`), then the API key saved
by `extend setup`, then the stored `extend login` session. Check what is
in effect with `extend config`.

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
