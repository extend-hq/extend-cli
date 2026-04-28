# extend-cli

> [!WARNING]
> This CLI is under active development. The command surface, flags, and
> output formats are not yet stable and may change without notice. Pin to
> a specific tag if you depend on a particular shape of output.

A CLI for [Extend](https://extend.ai).

## Install

    go install github.com/extend-hq/extend-cli/cmd/extend@latest

## Authenticate

    export EXTEND_API_KEY=sk_xxx

Optional:

    export EXTEND_BASE_URL=https://api.extend.ai   # override base URL
    export EXTEND_WORKSPACE_ID=ws_xxx              # for org-scoped keys

## Examples

    # extract structured data from a local PDF
    extend extract invoice.pdf --using ex_abc

    # parse to markdown
    extend parse contract.pdf > contract.md

    # run a workflow async; poll later
    RUN=$(extend run doc.pdf --workflow workflow_abc -o id)
    extend runs watch "$RUN"

    # filter JSON with jq
    extend extract invoice.pdf --using ex_abc --jq '.output.value.invoice_id' -o raw

    # batch
    extend extract batch invoices/*.pdf --using ex_abc

Inputs can be a local path (auto-uploads), a `file_xxx` ID, or an
`https://` URL.

## Commands

    extract | classify | split | parse | edit  <input> [flags]
    run <input> --workflow <id>                # workflow runs
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
`--jq '<expr>'` filters JSON before formatting. Data goes to stdout,
status to stderr. Honors `NO_COLOR` and `CLICOLOR_FORCE`.

## Async transport

The CLI always uses the asynchronous endpoints (`/extract_runs` etc.)
and polls. Short actions wait for terminal status by default; workflow
runs are async by default and require `--wait` to block.

## Develop

    mise install
    go test ./...

Integration tests live in `test/integration/` (separate module). They
require `EXTEND_BASE_URL` and `EXTEND_API_KEY`; `EXTEND_TEST_RUN_OPS=1`
enables the credit-spending tests.
</content>
