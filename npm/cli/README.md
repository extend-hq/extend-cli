# @extend-ai/cli

Command-line interface for [Extend](https://extend.ai).

## Install

```sh
npx @extend-ai/cli --help
```

For permanent install:

```sh
npm install -g @extend-ai/cli
extend --help
```

## How it works

This package is a thin wrapper that resolves to a platform-specific binary
shipped in a sibling package (`@extend-ai/cli-darwin-arm64`,
`@extend-ai/cli-linux-x64`, etc.). npm's `os` and `cpu` filters install only
the binary matching your machine.

After install, a postinstall step copies the native binary over a
placeholder so `extend` execs directly without a Node.js process in the
path.

If you install with `--ignore-scripts` or `--omit=optional`, the postinstall
won't run. In that case, invoke the fallback launcher:

```sh
node node_modules/@extend-ai/cli/cli-wrapper.cjs --help
```

## Source

Built from <https://github.com/extend-hq/extend-cli>. macOS binaries are
signed and notarized with Apple Developer ID.

## License

Apache-2.0
