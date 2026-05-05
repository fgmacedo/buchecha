#!/bin/sh
# Install bcc from a GitHub release.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/fgmacedo/buchecha/main/scripts/install.sh | sh
#   curl -sSL https://raw.githubusercontent.com/fgmacedo/buchecha/main/scripts/install.sh | sh -s -- -v
#
# Environment variables:
#   BCC_VERSION       Version tag to install (e.g. v0.1.0). Default: latest.
#   BCC_INSTALL_DIR   Install directory. Default: /usr/local/bin (falls back
#                     to $HOME/.local/bin if /usr/local/bin is not writable).
#
# License: MIT. Source: https://github.com/fgmacedo/buchecha

set -eu

REPO="fgmacedo/buchecha"
PROJECT="bcc"
VERBOSE=0

for arg in "$@"; do
  case "$arg" in
    -v|--verbose) VERBOSE=1 ;;
    -h|--help)
      sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '%s\n' "$*"; }
debug() { [ "$VERBOSE" -eq 1 ] && printf '> %s\n' "$*" >&2 || true; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

need uname
need tar
need mktemp

if command -v curl >/dev/null 2>&1; then
  DL="curl"
elif command -v wget >/dev/null 2>&1; then
  DL="wget"
else
  die "neither curl nor wget is available"
fi

if command -v sha256sum >/dev/null 2>&1; then
  SHA="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA="shasum -a 256"
else
  die "neither sha256sum nor shasum is available"
fi

os_raw=$(uname -s)
case "$os_raw" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux" ;;
  *) die "unsupported OS: $os_raw" ;;
esac

arch_raw=$(uname -m)
case "$arch_raw" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) die "unsupported architecture: $arch_raw" ;;
esac

debug "OS=$OS ARCH=$ARCH"

VERSION="${BCC_VERSION:-}"
if [ -z "$VERSION" ]; then
  debug "resolving latest release tag from GitHub API"
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
  if [ "$DL" = "curl" ]; then
    api_body=$(curl -sSL "$api_url")
  else
    api_body=$(wget -qO- "$api_url")
  fi
  VERSION=$(printf '%s\n' "$api_body" | grep -m1 '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || die "could not resolve latest release tag"
fi

case "$VERSION" in
  v*) VERSION_NO_V="${VERSION#v}" ;;
  *)  VERSION_NO_V="$VERSION"; VERSION="v$VERSION" ;;
esac

debug "VERSION=$VERSION"

archive="${PROJECT}_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
checksums="SHA256SUMS"
base="https://github.com/${REPO}/releases/download/${VERSION}"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t bcc-install)
trap 'rm -rf "$tmp"' EXIT

download() {
  src="$1"
  dst="$2"
  if [ "$DL" = "curl" ]; then
    curl -fSL --progress-bar "$src" -o "$dst"
  else
    wget -q --show-progress "$src" -O "$dst"
  fi
}

log "Downloading $archive"
download "${base}/${archive}" "${tmp}/${archive}"
download "${base}/${checksums}" "${tmp}/${checksums}"

log "Verifying checksum"
( cd "$tmp" && grep " ${archive}\$" "$checksums" > "${archive}.sha256" \
  && $SHA -c "${archive}.sha256" >/dev/null ) \
  || die "checksum verification failed"

log "Extracting"
tar -xzf "${tmp}/${archive}" -C "$tmp"

target_dir="${BCC_INSTALL_DIR:-/usr/local/bin}"
if [ ! -d "$target_dir" ]; then
  mkdir -p "$target_dir" 2>/dev/null || true
fi

if [ -w "$target_dir" ]; then
  install_cmd="install"
else
  fallback="$HOME/.local/bin"
  if [ -z "${BCC_INSTALL_DIR:-}" ]; then
    log "$target_dir is not writable; falling back to $fallback"
    target_dir="$fallback"
    mkdir -p "$target_dir"
    install_cmd="install"
  else
    log "$target_dir is not writable; trying with sudo"
    if command -v sudo >/dev/null 2>&1; then
      install_cmd="sudo install"
    else
      die "no write permission for $target_dir and sudo is not available; set BCC_INSTALL_DIR to a writable path"
    fi
  fi
fi

debug "installing to $target_dir"
$install_cmd -m 0755 "${tmp}/${PROJECT}" "${target_dir}/${PROJECT}"

case ":$PATH:" in
  *":$target_dir:"*) in_path=1 ;;
  *) in_path=0 ;;
esac

log "Installed ${PROJECT} ${VERSION} to ${target_dir}/${PROJECT}"
if [ "$in_path" -eq 0 ]; then
  log "note: $target_dir is not in your PATH"
fi

if [ -x "${target_dir}/${PROJECT}" ]; then
  "${target_dir}/${PROJECT}" --version || true
fi
