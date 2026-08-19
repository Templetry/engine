#!/bin/sh
# Installs the templetry CLI and the templetry-mcp server on macOS or Linux.
#
#   curl -fsSL https://templetry.dev/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/Templetry/engine/main/install.sh | sh
#
# Environment:
#   TEMPLETRY_VERSION   tag to install (default: the latest release)
#   TEMPLETRY_BIN_DIR   where to put the binaries (default: see below)
#
# POSIX sh on purpose: this has to run under dash on a minimal Debian image
# as happily as under bash on a Mac.
set -eu

REPO="Templetry/engine"
RED=""; DIM=""; BOLD=""; OFF=""
if [ -t 1 ] && [ -z "${NO_COLOR-}" ]; then
  RED=$(printf '\033[31m'); DIM=$(printf '\033[2m')
  BOLD=$(printf '\033[1m'); OFF=$(printf '\033[0m')
fi

say()  { printf '%s\n' "$*"; }
step() { printf '%s→%s %s\n' "$DIM" "$OFF" "$*"; }
die()  { printf '%serror:%s %s\n' "$RED" "$OFF" "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required and was not found."
}

# ── What are we on ───────────────────────────────────────────────────────────

os=$(uname -s)
case "$os" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported operating system: $os. Windows users: scoop install templetry" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch. Templetry ships amd64 and arm64." ;;
esac

need curl
need mktemp

# ── Which version ────────────────────────────────────────────────────────────

version="${TEMPLETRY_VERSION-}"
if [ -z "$version" ]; then
  step "Looking up the latest release"
  # Read the tag without needing jq: the field is on its own in the JSON.
  version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -1)
  [ -n "$version" ] || die "could not determine the latest release. Set TEMPLETRY_VERSION to install a specific tag."
fi

# ── Where it goes ────────────────────────────────────────────────────────────

# Prefer somewhere already on PATH that we can write without sudo, because an
# installer that silently needs root is an installer that fails at the worst
# moment.
bin_dir="${TEMPLETRY_BIN_DIR-}"
if [ -z "$bin_dir" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then
    bin_dir=/usr/local/bin
  else
    bin_dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$bin_dir" || die "cannot create $bin_dir. Set TEMPLETRY_BIN_DIR to a writable directory."
[ -w "$bin_dir" ] || die "$bin_dir is not writable. Set TEMPLETRY_BIN_DIR to somewhere you own."

# ── Download, verify, install ────────────────────────────────────────────────

base="https://github.com/$REPO/releases/download/$version"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

step "Fetching checksums for $version"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" \
  || die "no release assets for $version. Check https://github.com/$REPO/releases"

verify() { # verify <file> <asset-name>
  want=$(awk -v a="$2" '$2 == a { print $1 }' "$tmp/SHA256SUMS")
  [ -n "$want" ] || die "$2 is not listed in SHA256SUMS for $version."

  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$1" | cut -d' ' -f1)
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$1" | cut -d' ' -f1)
  else
    say "${DIM}   no sha256 tool found; skipping verification${OFF}"
    return 0
  fi

  [ "$got" = "$want" ] || die "checksum mismatch for $2.
  expected $want
  got      $got
This is worth reporting: https://github.com/$REPO/issues"
}

for tool in templetry templetry-mcp; do
  asset="$tool-$os-$arch"
  step "Downloading $asset"
  curl -fsSL "$base/$asset" -o "$tmp/$tool" || die "could not download $asset."
  verify "$tmp/$tool" "$asset"
  chmod +x "$tmp/$tool"
  mv -f "$tmp/$tool" "$bin_dir/$tool"
done

# ── Report honestly ──────────────────────────────────────────────────────────

say ""
say "${BOLD}templetry $version${OFF} installed in $bin_dir"

case ":${PATH}:" in
  *":$bin_dir:"*)
    say ""
    say "Try it:"
    say "  ${BOLD}templetry list${OFF}"
    ;;
  *)
    say ""
    say "${BOLD}$bin_dir is not on your PATH.${OFF} Add it:"
    say ""
    say "  export PATH=\"$bin_dir:\$PATH\""
    say ""
    say "Put that line in your shell profile to make it stick."
    ;;
esac
