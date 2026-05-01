#!/usr/bin/env node
// Fallback launcher for the extendcli wrapper package.
//
// Normally the postinstall script copies the native binary over
// bin/extend.exe, so this file is never invoked. It exists for environments
// where postinstall doesn't run (--ignore-scripts) — users can run
// `node cli-wrapper.cjs` directly and pay the Node-process overhead as the
// price.
//
// Platform detection + PLATFORMS map is duplicated in install.cjs — keep in
// sync.

const { spawnSync } = require('child_process')
const { arch, constants } = require('os')
const path = require('path')

const PACKAGE_PREFIX = '@extend-ai/cli'
const BINARY_NAME = 'extend'
const WRAPPER_NAME = require('./package.json').name

const PLATFORMS = {
  'darwin-arm64': { pkg: PACKAGE_PREFIX + '-darwin-arm64', bin: BINARY_NAME },
  'darwin-x64': { pkg: PACKAGE_PREFIX + '-darwin-x64', bin: BINARY_NAME },
  'linux-x64': { pkg: PACKAGE_PREFIX + '-linux-x64', bin: BINARY_NAME },
  'linux-arm64': { pkg: PACKAGE_PREFIX + '-linux-arm64', bin: BINARY_NAME },
  'win32-x64': {
    pkg: PACKAGE_PREFIX + '-win32-x64',
    bin: BINARY_NAME + '.exe',
  },
}

function getBinaryPath() {
  const platformKey = process.platform + '-' + arch()
  const info = PLATFORMS[platformKey]
  if (!info) {
    console.error(
      `[${WRAPPER_NAME}] Unsupported platform: ${process.platform} ${arch()}.`,
    )
    console.error(`  Supported: ${Object.keys(PLATFORMS).join(', ')}`)
    process.exit(1)
  }
  try {
    const pkgDir = path.dirname(require.resolve(info.pkg + '/package.json'))
    return path.join(pkgDir, 'bin', info.bin)
  } catch {
    console.error(
      `[${WRAPPER_NAME}] Could not find native binary package "${info.pkg}".`,
    )
    console.error('  Try reinstalling: npm install ' + WRAPPER_NAME)
    process.exit(1)
  }
}

function main() {
  const binaryPath = getBinaryPath()
  const result = spawnSync(binaryPath, process.argv.slice(2), {
    stdio: 'inherit',
  })
  if (result.error) {
    console.error(
      `[${WRAPPER_NAME}] Failed to execute native binary at ` + binaryPath,
    )
    console.error('  ' + result.error.message)
    process.exit(1)
  }
  if (result.signal) {
    // Node ignores some signals (e.g. SIGPIPE → SIG_IGN) so re-raising is
    // unreliable. Use POSIX 128+signum convention instead.
    const signum = constants.signals[result.signal] ?? 0
    process.exit(128 + signum)
  }
  process.exit(result.status ?? 1)
}

main()
