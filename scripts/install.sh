#!/bin/sh
# Install axx: https://axx.nimbusxr.us
#
#   curl -fsSL https://axx.nimbusxr.us/install.sh | sh
#
# Environment:
#   AXX_VERSION      version to install (e.g. 0.1.0, v0.1.0, nightly); default: newest release
#   AXX_INSTALL_DIR  where to put the binary; default: /usr/local/bin if writable, else ~/.local/bin
#   GITHUB_TOKEN     optional, avoids GitHub API rate limits
#
# Releases are GitHub pre-releases during the 0.x beta, so the newest release
# is resolved from the releases list (GitHub's /latest skips pre-releases).
set -eu

REPO="nimbusxr/axx"
API="https://api.github.com/repos/$REPO"

say() { printf 'axx-install: %s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

need uname
need tar
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL ${GITHUB_TOKEN:+-H "Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
  download() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- ${GITHUB_TOKEN:+--header="Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
  download() { wget -qO "$2" "$1"; }
else
  die "curl or wget is required"
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  msys*|mingw*|cygwin*) die "use install.ps1 on Windows: irm https://axx.nimbusxr.us/install.ps1 | iex" ;;
  *) die "unsupported OS: $os" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

version=${AXX_VERSION:-}
if [ -z "$version" ]; then
  # First non-draft, non-nightly release (pre-releases included).
  version=$(fetch "$API/releases?per_page=20" | tr ',' '\n' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | grep -v '^nightly$' | head -n 1)
  [ -n "$version" ] || die "could not determine the newest release; set AXX_VERSION"
fi
case "$version" in
  nightly) tag=nightly ;;
  v*) tag=$version; version=${version#v} ;;
  *) tag=v$version ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

base="https://github.com/$REPO/releases/download/$tag"
if [ "$tag" = nightly ]; then
  archive=$(fetch "$API/releases/tags/nightly" | tr ',' '\n' | sed -n "s/.*\"name\": *\"\(axx_[^\"]*_${os}_${arch}\.tar\.gz\)\".*/\1/p" | head -n 1)
  [ -n "$archive" ] || die "no nightly build for $os/$arch"
else
  archive="axx_${version}_${os}_${arch}.tar.gz"
fi

say "downloading $archive ($tag)"
download "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
download "$base/checksums.txt" "$tmp/checksums.txt" || die "download failed: checksums.txt"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
[ -n "$expected" ] || die "$archive is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || die "checksum mismatch for $archive"
say "checksum verified"

if command -v gh >/dev/null 2>&1 && [ "$tag" != nightly ]; then
  if gh attestation verify "$tmp/checksums.txt" --repo "$REPO" >/dev/null 2>&1; then
    say "build provenance verified (gh attestation)"
  else
    say "note: could not verify build provenance with gh (continuing; checksum matched)"
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp" axx
dir=${AXX_INSTALL_DIR:-}
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/axx" "$dir/axx" 2>/dev/null || { cp "$tmp/axx" "$dir/axx" && chmod 0755 "$dir/axx"; }
ln -sf "$dir/axx" "$dir/axxeptance" 2>/dev/null || true

say "installed $("$dir/axx" version 2>/dev/null || echo axx) to $dir/axx (alias: axxeptance)"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "add $dir to your PATH, e.g.: export PATH=\"$dir:\$PATH\"" ;;
esac
say "next: axx init   (docs: https://axx.nimbusxr.us)"
