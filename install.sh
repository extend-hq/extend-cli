#!/bin/sh
set -eu

REPO="${EXTEND_GITHUB_REPO:-extend-hq/extend-cli}"
VERSION="${EXTEND_VERSION:-latest}"
INSTALL_DIR="${EXTEND_INSTALL_DIR:-}"
SKIP_CHECKSUM="${EXTEND_SKIP_CHECKSUM:-0}"
SKIP_SKILL_INSTALL="${EXTEND_SKIP_SKILL_INSTALL:-0}"
NO_MODIFY_PATH="${EXTEND_NO_MODIFY_PATH:-0}"
DIR_AUTO=0

usage() {
  cat <<'EOF'
Install the Extend CLI.

Usage:
  curl -fsSL https://extend.ai/install.sh | sh
  curl -fsSL https://extend.ai/install.sh | sh -s -- --version v0.1.0 --bin-dir ~/.local/bin

Options:
  -b, --bin-dir DIR     Directory to install extend into. Default: upgrade an
                        existing extend install in place; otherwise the first
                        of ~/.local/bin, ~/bin, /usr/local/bin that is on
                        PATH and writable (Homebrew-owned dirs are never
                        used); otherwise ~/.local/bin
  -v, --version VERSION Version to install, e.g. v0.1.0 (default: latest)
      --skip-checksum   Skip SHA256 verification
      --skip-skill-install
                        Skip automatic agent skill installation
      --no-modify-path  Don't add the install dir to PATH via your shell
                        profile when no usable dir is on PATH already
  -h, --help            Show this help

Environment:
  EXTEND_INSTALL_DIR    Same as --bin-dir
  EXTEND_VERSION        Same as --version
  EXTEND_SKIP_CHECKSUM  Set to 1 to skip SHA256 verification
  EXTEND_SKIP_SKILL_INSTALL
                        Set to 1 to skip automatic skill installation
  EXTEND_NO_MODIFY_PATH Set to 1 to never touch shell profiles
EOF
}

log() {
  printf '%s\n' "extend-installer: $*" >&2
}

die() {
  log "error: $*"
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

need_cmd() {
  has_cmd "$1" || die "required command not found: $1"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -b|--bin-dir)
      [ "$#" -ge 2 ] || die "$1 requires a directory"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --bin-dir=*)
      INSTALL_DIR=${1#*=}
      shift
      ;;
    -v|--version)
      [ "$#" -ge 2 ] || die "$1 requires a version"
      VERSION="$2"
      shift 2
      ;;
    --version=*)
      VERSION=${1#*=}
      shift
      ;;
    --skip-checksum)
      SKIP_CHECKSUM=1
      shift
      ;;
    --skip-skill-install)
      SKIP_SKILL_INSTALL=1
      shift
      ;;
    --no-modify-path)
      NO_MODIFY_PATH=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      die "unknown option: $1"
      ;;
  esac
done

need_cmd uname
need_cmd tar
need_cmd mktemp
need_cmd awk

if has_cmd curl; then
  DOWNLOADER=curl
elif has_cmd wget; then
  DOWNLOADER=wget
else
  die "curl or wget is required"
fi

http_get() {
  if [ "$DOWNLOADER" = curl ]; then
    curl -fsSL "$1"
  else
    wget -qO- "$1"
  fi
}

download_to() {
  if [ "$DOWNLOADER" = curl ]; then
    curl -fsSL "$1" -o "$2"
  else
    wget -qO "$2" "$1"
  fi
}

resolve_version() {
  if [ "$VERSION" = latest ]; then
    latest_json=$(http_get "https://api.github.com/repos/${REPO}/releases/latest") || die "failed to resolve latest release"
    tag=$(printf '%s\n' "$latest_json" | awk -F'"' '/"tag_name"[[:space:]]*:/ { print $4; exit }')
    [ -n "$tag" ] || die "could not find latest release tag"
  else
    tag=$VERSION
  fi

  case "$tag" in
    v*) printf '%s\n' "$tag" ;;
    *) printf 'v%s\n' "$tag" ;;
  esac
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin\n' ;;
    Linux) printf 'linux\n' ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

hash_file() {
  if has_cmd sha256sum; then
    sha256sum "$1" | awk '{print $1}'
  elif has_cmd shasum; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif has_cmd openssl; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  else
    die "sha256sum, shasum, or openssl is required to verify checksums"
  fi
}

on_path() {
  case ":${PATH:-}:" in
    *:"$1":*) return 0 ;;
    *) return 1 ;;
  esac
}

# usable_dir: we can drop a binary in $1 without sudo and without fighting a
# package manager. A dir containing a `brew` executable is Homebrew's prefix
# bin (/opt/homebrew/bin, or /usr/local/bin on Intel Macs) — never install
# foreign binaries there. An `extend` symlink there is a manager's (Homebrew
# links are symlinks) — never overwrite it. An existing dir must be
# writable; a missing dir only counts under $HOME (we can mkdir it).
usable_dir() {
  if [ -e "$1/brew" ] || [ -L "$1/extend" ]; then
    return 1
  fi
  if [ -d "$1" ]; then
    [ -w "$1" ]
  else
    case "$1" in
      "${HOME:-/nonexistent}"/*) return 0 ;;
      *) return 1 ;;
    esac
  fi
}

# choose_install_dir picks INSTALL_DIR (and a human reason) when --bin-dir /
# EXTEND_INSTALL_DIR wasn't given:
#   1. Upgrade in place: the `extend` the shell already resolves (a regular
#      file in a writable dir) is replaced where it lives, so an old install
#      can never shadow the new one.
#   2. First candidate dir that is on PATH and usable — the install works in
#      the current shell with no profile edits (macOS does not put
#      ~/.local/bin on PATH by default).
#   3. Fall back to ~/.local/bin (the historical default) plus a warning.
choose_install_dir() {
  DIR_AUTO=1
  existing=$(command -v extend 2>/dev/null || true)
  if [ -n "$existing" ] && [ ! -L "$existing" ]; then
    dir=${existing%/*}
    if usable_dir "$dir"; then
      INSTALL_DIR=$dir
      INSTALL_REASON="replacing the existing extend at $existing"
      return
    fi
  fi

  if [ -n "${HOME:-}" ]; then
    fallback="$HOME/.local/bin"
    candidates="$HOME/.local/bin:$HOME/bin:/usr/local/bin"
  else
    fallback=/usr/local/bin
    candidates=/usr/local/bin
  fi
  candidates=${EXTEND_INSTALL_CANDIDATES:-$candidates}

  old_ifs=${IFS:-}
  IFS=:
  # shellcheck disable=SC2086 # word-splitting on : is the point
  set -- $candidates
  IFS=$old_ifs
  for dir in "$@"; do
    [ -n "$dir" ] || continue
    if on_path "$dir" && usable_dir "$dir"; then
      INSTALL_DIR=$dir
      INSTALL_REASON="already on PATH"
      return
    fi
  done

  INSTALL_DIR=$fallback
  INSTALL_REASON="default"
}

install_binary() {
  if [ -z "$INSTALL_DIR" ]; then
    choose_install_dir
    log "installing to $INSTALL_DIR ($INSTALL_REASON)"
  fi

  mkdir -p "$INSTALL_DIR" || die "failed to create $INSTALL_DIR"
  target="$INSTALL_DIR/extend"

  if has_cmd install; then
    install -m 755 "$tmp/extend" "$target" || die "failed to install to $target"
  else
    cp "$tmp/extend" "$target" || die "failed to install to $target"
    chmod 755 "$target" || die "failed to mark $target executable"
  fi
}

run_setup() {
  # Hand onboarding to the CLI: `extend setup` runs the interactive wizard
  # when it has a terminal, otherwise prints setup guidance and installs the
  # agent skill. Under `curl | sh` the script's stdin is the install script
  # itself, so a human is detected via stdout being a terminal and the wizard
  # is fed the controlling terminal (/dev/tty). --skip-skill-install is
  # forwarded as a CLI flag (truthy = pass the flag). Best-effort: a setup
  # hiccup must not fail an otherwise-successful install.
  set -- setup
  if [ "$SKIP_SKILL_INSTALL" = 1 ]; then
    set -- "$@" --skip-skill-install
  fi
  if [ -t 1 ] && [ -r /dev/tty ]; then
    "$target" "$@" </dev/tty || true
  else
    "$target" "$@" || true
  fi
}

warn_if_shadowed() {
  # The chosen dir is on PATH, but a different `extend` may sit ahead of it.
  # That causes a confusing failure: `extend setup` (run here by absolute
  # path) configures this binary while the user's shell runs the other one,
  # so the wizard "works" yet commands report the API key unset.
  found=$(command -v extend 2>/dev/null || true)
  if [ -n "$found" ] && [ "$found" != "$target" ]; then
    log "warning: a different 'extend' shadows the one just installed on PATH"
    log "  will run:   $found"
    log "  installed:  $target"
    log "fix: remove the other binary or put $INSTALL_DIR earlier on PATH, then run 'hash -r'"
  fi
}

# ensure_on_path runs when the chosen dir is NOT on PATH. A stock macOS
# shell has no user-writable dir on PATH at all, so for an auto-chosen dir
# the fix is one guarded PATH line appended to the file the user's login
# shell actually reads (selected by $SHELL, since that is shell-specific,
# not OS-specific). An explicit --bin-dir/EXTEND_INSTALL_DIR or
# --no-modify-path/EXTEND_NO_MODIFY_PATH downgrades to a warning — we don't
# second-guess a user who picked the location or opted out.
ensure_on_path() {
  found=$(command -v extend 2>/dev/null || true)
  if [ "$DIR_AUTO" != 1 ] || [ "$NO_MODIFY_PATH" = 1 ] || [ -z "${HOME:-}" ]; then
    log "warning: $INSTALL_DIR is not on PATH"
    log "add it with: export PATH=\"$INSTALL_DIR:\$PATH\""
    if [ -n "$found" ] && [ "$found" != "$target" ]; then
      log "until then, 'extend' runs: $found"
    fi
    return 0
  fi

  # Write $HOME-relative dirs symbolically so the profile line survives a
  # home rename and reads like what a user would write by hand.
  case "$INSTALL_DIR" in
    "$HOME"/*) dir_expr="\$HOME${INSTALL_DIR#"$HOME"}" ;;
    *) dir_expr=$INSTALL_DIR ;;
  esac

  # Which file the user's login shell reads:
  #   zsh   ${ZDOTDIR:-~}/.zprofile — macOS terminals open login shells
  #   bash  first existing of .bash_profile/.bash_login/.profile — bash
  #         skips .profile when .bash_profile exists
  #   fish  conf.d snippet — fish never reads POSIX profiles
  #   else  ~/.profile
  line="export PATH=\"$dir_expr:\$PATH\""
  case "${SHELL:-}" in
    */zsh)
      profile="${ZDOTDIR:-$HOME}/.zprofile"
      ;;
    */bash)
      profile="$HOME/.profile"
      for name in .bash_profile .bash_login .profile; do
        if [ -f "$HOME/$name" ]; then
          profile="$HOME/$name"
          break
        fi
      done
      ;;
    */fish)
      profile="${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/extend.fish"
      line="fish_add_path \"$dir_expr\""
      ;;
    *)
      profile="$HOME/.profile"
      ;;
  esac

  if [ -f "$profile" ] && grep -qsF "$dir_expr" "$profile"; then
    log "$profile already references $dir_expr"
  else
    mkdir -p "${profile%/*}" 2>/dev/null || true
    # Lead with a newline when appending to a non-empty file: a profile
    # whose last line is unterminated must not have our line glued onto it.
    if [ -s "$profile" ]; then
      fmt='\n%s\n'
    else
      fmt='%s\n'
    fi
    # shellcheck disable=SC2059 # fmt is one of two fixed format strings
    if printf "$fmt" "$line" >>"$profile" 2>/dev/null; then
      log "added $INSTALL_DIR to PATH in $profile"
    else
      log "warning: could not write $profile"
      log "add it yourself: export PATH=\"$INSTALL_DIR:\$PATH\""
      return 0
    fi
  fi
  log "open a new shell, or run now: export PATH=\"$INSTALL_DIR:\$PATH\""
}

tag=$(resolve_version)
goos=$(detect_os)
goarch=$(detect_arch)
archive="extend_${tag}_${goos}_${goarch}.tar.gz"
official_base_url="https://github.com/${REPO}/releases/download/${tag}"
base_url="${EXTEND_RELEASE_BASE_URL:-$official_base_url}"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t extend-install)
trap 'rm -rf "$tmp"' 0 INT HUP TERM

log "downloading ${archive}"
download_to "$base_url/$archive" "$tmp/$archive" || die "failed to download $archive"

if [ "$SKIP_CHECKSUM" != 1 ]; then
  download_to "$official_base_url/SHA256SUMS" "$tmp/SHA256SUMS" || die "failed to download SHA256SUMS"
  expected=$(awk -v file="$archive" '$2 == file { print $1; exit }' "$tmp/SHA256SUMS")
  [ -n "$expected" ] || die "checksum for $archive not found"
  actual=$(hash_file "$tmp/$archive")
  [ "$expected" = "$actual" ] || die "checksum mismatch for $archive"
fi

tar -xzf "$tmp/$archive" -C "$tmp" || die "failed to extract $archive"
chmod 755 "$tmp/extend" || die "failed to mark downloaded binary executable"
"$tmp/extend" --version >/dev/null || die "downloaded binary failed to run"

install_binary

log "installed $tag to $target"
if on_path "$INSTALL_DIR"; then
  warn_if_shadowed
else
  ensure_on_path
fi

run_setup
