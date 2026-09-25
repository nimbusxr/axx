#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Checks what `goreleaser release --snapshot` put in dist/ before anything is published:
# the archives, their checksums and SBOMs, the Homebrew cask, the binaries and the images.
#
#   scripts/release-check.sh [dist]
#
# SBOMs and images are checked when the build made them (it skips them without syft or
# docker); set RELEASE_CHECK_STRICT=1 to require them, as the release dry run does.
set -euo pipefail

dist=${1:-dist}
strict=${RELEASE_CHECK_STRICT:-0}
repo=https://github.com/nimbusxr/axx/releases/download/
targets=(linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64)
failed=0

ok() { printf '  ok    %s\n' "$*"; }
fail() {
	printf '  FAIL  %s\n' "$*"
	failed=1
}

version=$(jq -r .version "$dist/metadata.json")
echo "axx $version in $dist"

# Archives: one per target, each with the binary and the license files.
for t in "${targets[@]}"; do
	ext=tar.gz
	bin=axx
	if [[ $t == windows_* ]]; then
		ext=zip
		bin=axx.exe
	fi
	archive="$dist/axx_${version}_${t}.${ext}"
	if [[ ! -f $archive ]]; then
		fail "no archive for $t"
		continue
	fi
	if [[ $ext == zip ]]; then
		files=$(unzip -Z1 "$archive" | sort | tr '\n' ' ')
	else
		files=$(tar tzf "$archive" | sort | tr '\n' ' ')
	fi
	want=$(printf '%s\n' LICENSE NOTICE README.md "$bin" | sort | tr '\n' ' ')
	if [[ $files == "$want" ]]; then ok "$(basename "$archive"): $files"; else fail "$(basename "$archive") holds '$files', want '$want'"; fi
done

# Checksums: every archive and SBOM, and they match.
listed=$(awk '{print $2}' "$dist/checksums.txt" | sort)
want=$(find "$dist" -maxdepth 1 \( -name '*.tar.gz' -o -name '*.zip' -o -name '*.sbom.json' \) -exec basename {} \; | sort)
if [[ -n $listed && $listed == "$want" ]]; then
	ok "checksums.txt lists the $(wc -l <<<"$listed" | tr -d ' ') archives and SBOMs"
else
	fail "checksums.txt lists '$(tr '\n' ' ' <<<"$listed")', want '$(tr '\n' ' ' <<<"$want")'"
fi
if (cd "$dist" && sha256sum --check --quiet checksums.txt 2>/dev/null || shasum -a 256 --check --quiet checksums.txt); then
	ok "checksums match"
else
	fail "checksums do not match"
fi

# SBOMs: one per archive.
sboms=$(find "$dist" -maxdepth 1 -name '*.sbom.json' | wc -l | tr -d ' ')
if [[ $sboms -eq ${#targets[@]} ]]; then
	ok "$sboms SBOMs"
elif [[ $sboms -eq 0 && $strict != 1 ]]; then
	echo "  skip  SBOMs (not built)"
else
	fail "$sboms SBOMs, want ${#targets[@]}"
fi

# The Homebrew cask: downloads from this repository's releases, and runs on macOS.
cask="$dist/homebrew/Casks/axx.rb"
if [[ ! -f $cask ]]; then
	fail "no Homebrew cask"
else
	urls=$(grep -oE 'url "[^"]+"' "$cask" | cut -d'"' -f2)
	bad=$(grep -vc "^$repo" <<<"$urls" || true)
	if [[ -n $urls && $bad -eq 0 ]]; then ok "cask downloads from $repo"; else fail "cask URLs outside $repo: $urls"; fi
	if grep -q 'com.apple.quarantine' "$cask"; then ok "cask clears the quarantine flag"; else fail "cask does not clear the quarantine flag"; fi
	if grep -q "version \"$version\"" "$cask"; then ok "cask version $version"; else fail "cask version is not $version"; fi
fi

# The binary for this machine runs and reports the version.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case $arch in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
tar xzf "$dist/axx_${version}_${os}_${arch}.tar.gz" -C "$tmp" axx
if out=$("$tmp/axx" version) && [[ $out == "axx v$version "* ]]; then ok "binary: $out"; else fail "binary says '${out:-}'"; fi

# The images run and report the version.
if command -v docker >/dev/null && docker image inspect "ghcr.io/nimbusxr/axx:v$version-amd64" >/dev/null 2>&1; then
	for a in amd64 arm64; do
		image="ghcr.io/nimbusxr/axx:v$version-$a"
		if out=$(docker run --rm --platform "linux/$a" "$image" version) && [[ $out == "axx v$version "* ]]; then ok "$image: $out"; else fail "$image says '${out:-}'"; fi
	done
elif [[ $strict == 1 ]]; then
	fail "no images"
else
	echo "  skip  images (not built)"
fi

if [[ $failed -ne 0 ]]; then
	echo "release check failed"
	exit 1
fi
echo "release check passed"
