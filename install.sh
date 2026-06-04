#!/bin/sh
set -eu

REPO="${EXTEND_GITHUB_REPO:-extend-hq/extend-cli}"
VERSION="${EXTEND_VERSION:-latest}"
INSTALL_DIR="${EXTEND_INSTALL_DIR:-}"
SKIP_CHECKSUM="${EXTEND_SKIP_CHECKSUM:-0}"

usage() {
  cat <<'EOF'
Install the Extend CLI.

Usage:
  curl -fsSL https://extend.ai/install.sh | sh
  curl -fsSL https://extend.ai/install.sh | sh -s -- --version v0.1.0 --bin-dir ~/.local/bin

Options:
  -b, --bin-dir DIR     Directory to install extend into (default: ~/.local/bin)
  -v, --version VERSION Version to install, e.g. v0.1.0 (default: latest)
      --skip-checksum   Skip SHA256 verification
  -h, --help            Show this help

Environment:
  EXTEND_INSTALL_DIR    Same as --bin-dir
  EXTEND_VERSION        Same as --version
  EXTEND_SKIP_CHECKSUM  Set to 1 to skip SHA256 verification
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

install_binary() {
  if [ -z "$INSTALL_DIR" ]; then
    if [ -n "${HOME:-}" ]; then
      INSTALL_DIR="$HOME/.local/bin"
    else
      INSTALL_DIR=/usr/local/bin
    fi
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
case ":${PATH:-}:" in
  *:"$INSTALL_DIR":*) ;;
  *)
    log "warning: $INSTALL_DIR is not on PATH"
    log 'add it with: export PATH="$HOME/.local/bin:$PATH"'
    ;;
esac
