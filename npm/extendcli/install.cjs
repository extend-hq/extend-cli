#!/usr/bin/env node
// Postinstall for the extendcli wrapper package (name in ./package.json).
//
// Detects the platform, finds the matching native binary from
// optionalDependencies, and copies it over the bin/extend.exe placeholder.
// After this runs, `extend` execs the native binary directly — no Node.js
// process stays resident.
//
// If the native package isn't present (--omit=optional), prints instructions
// and leaves the placeholder stub in place. cli-wrapper.cjs (same directory)
// can be invoked manually as a fallback that keeps working via
// require.resolve + spawn.
//
// Platform detection + PLATFORMS map is duplicated in cli-wrapper.cjs — keep
// in sync.

const {
  copyFileSync,
  linkSync,
  unlinkSync,
  chmodSync,
  readFileSync,
  writeFileSync,
  statSync,
} = require('fs')
const { arch } = require('os')
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

function getPlatformKey() {
  return process.platform + '-' + arch()
}

function placeBinary(src, dest) {
  // Try hardlink first (instant, zero extra disk; src and dest are both under
  // node_modules/ so same-filesystem is the common case). We attempt the link
  // BEFORE touching dest — if src is missing (partial extraction) the first
  // linkSync throws ENOENT and the fallback stub stays.
  try {
    linkSync(src, dest)
  } catch (err) {
    if (err.code === 'EEXIST') {
      // Read the stub before unlinking so we can restore it if both link and
      // copy fail (ENOSPC, NFS error mid-copy). Only read if dest is
      // stub-sized — on re-install dest is the real binary.
      const stub = statSync(dest).size < 4096 ? readFileSync(dest) : null
      unlinkSync(dest)
      try {
        linkSync(src, dest)
      } catch {
        try {
          copyFileSync(src, dest)
        } catch (copyErr) {
          if (stub) {
            try {
              writeFileSync(dest, stub, { mode: 0o755 })
            } catch {
              // best-effort restore
            }
          }
          throw copyErr
        }
      }
    } else if (err.code === 'EXDEV' || err.code === 'EPERM') {
      // Cross-device or no-link-perms — copyFileSync overwrites existing dest.
      copyFileSync(src, dest)
    } else {
      throw err
    }
  }
  if (process.platform !== 'win32') {
    chmodSync(dest, 0o755)
  }
}

function main() {
  const platformKey = getPlatformKey()
  const info = PLATFORMS[platformKey]

  if (!info) {
    console.error(
      `[${WRAPPER_NAME} postinstall] Unsupported platform: ${process.platform} ${arch()}`,
    )
    console.error(`  Supported: ${Object.keys(PLATFORMS).join(', ')}`)
    return
  }

  let src
  try {
    const pkgDir = path.dirname(require.resolve(info.pkg + '/package.json'))
    src = path.join(pkgDir, 'bin', info.bin)
  } catch {
    console.error(
      `[${WRAPPER_NAME} postinstall] Native package "${info.pkg}" not found.`,
    )
    console.error(
      '  This happens with --omit=optional or when the download failed.',
    )
    console.error(
      '  The `extend` command will not work until you reinstall with optional deps.',
    )
    console.error('  Fallback: node ' + path.join(__dirname, 'cli-wrapper.cjs'))
    return
  }

  // Always write to bin/extend.exe — the package.json bin field points here.
  // The .exe extension + no-shebang stub makes npm's cmd-shim (generated at
  // install time, before postinstall) emit a direct exec on Windows; Unix
  // ignores the extension. Same pattern as Bun's npm package.
  const dest = path.join(__dirname, 'bin', 'extend.exe')

  try {
    placeBinary(src, dest)
  } catch (err) {
    console.error(
      `[${WRAPPER_NAME} postinstall] Failed to place binary: ${err.message}`,
    )
    console.error('  Fallback: node ' + path.join(__dirname, 'cli-wrapper.cjs'))
    process.exitCode = 1
  }
}

main()
