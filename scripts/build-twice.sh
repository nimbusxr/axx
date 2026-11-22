#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# One commit gives the same release files, bit for bit, whoever builds them, where and when.
# This makes them twice, as GoReleaser makes a release's, and compares them: once in this
# checkout, and once in a second checkout of the same commit in another folder.
#
#   scripts/build-twice.sh linux|darwin|windows   # that system's binaries, amd64 and arm64
#   scripts/build-twice.sh archives               # every target's archive, and checksums.txt
#
# A system's second build starts from an empty build cache, so that it reuses nothing the
# first one compiled. The archives' two builds share the cache: what they compare is how the
# files are packed. Nothing is signed or published.
#
# A difference means something of the machine or the time got into a file: a path (the builds
# use -trimpath), a clock (the files carry their commit's time), or an order that changes from
# one run to the next.
set -euo pipefail
cd "$(dirname "$0")/.."

part=${1:-}
case $part in
linux | darwin | windows | archives) ;;
*)
	echo "usage: scripts/build-twice.sh linux|darwin|windows|archives" >&2
	exit 2
	;;
esac

if ! git diff --quiet HEAD --; then
	echo "build-twice: commit the changes first: the second build is of the commit, not of this folder" >&2
	exit 2
fi

tag=$(scripts/cli-tag.sh)
work=$(mktemp -d)
second=$work/second
git worktree add --quiet --detach "$second" HEAD
trap 'git worktree remove --force "$second" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

if command -v sha256sum >/dev/null 2>&1; then
	sum() { sha256sum "$1" | cut -d' ' -f1; }
else
	sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

# Runs GoReleaser in a folder, quietly unless it fails.
#   goreleaser_in <folder> <arguments>
goreleaser_in() {
	local dir=$1
	shift
	if ! (cd "$dir" && GORELEASER_CURRENT_TAG=$tag goreleaser "$@") >"$work/log" 2>&1; then
		cat "$work/log" >&2
		echo "build-twice: goreleaser $* failed in $dir" >&2
		exit 1
	fi
}

failed=0
compared=0

# Compares one file of this checkout's dist with the second checkout's.
same() {
	local file=$1 here there
	if [[ ! -f $second/$file ]]; then
		printf '  DIFFERENT  %s: the second build made none\n' "$file"
		failed=1
		return
	fi
	here=$(sum "$file")
	there=$(sum "$second/$file")
	compared=$((compared + 1))
	if [[ $here == "$there" ]]; then
		printf '  same       %s  %s\n' "$here" "$file"
	else
		printf '  DIFFERENT  %s\n             first:  %s\n             second: %s\n' "$file" "$here" "$there"
		failed=1
	fi
}

if [[ $part == archives ]]; then
	echo "the archives of every target, made twice"
	for dir in . "$second"; do
		goreleaser_in "$dir" release --snapshot --clean --skip=docker,homebrew,sign,sbom,notarize
	done
	for file in dist/*.tar.gz dist/*.zip dist/checksums.txt; do
		same "$file"
	done
	want=7
else
	echo "the $part binaries, built twice: the second time in another folder, from an empty build cache"
	for arch in amd64 arm64; do
		GOOS=$part GOARCH=$arch goreleaser_in . build --snapshot --clean --single-target
		GOOS=$part GOARCH=$arch GOCACHE=$work/cache goreleaser_in "$second" build --snapshot --clean --single-target
		found=0
		for file in dist/axx_"${part}_${arch}"_*/axx*; do
			[[ -f $file ]] || continue
			same "$file"
			found=1
		done
		if [[ $found == 0 ]]; then
			printf '  DIFFERENT  %s/%s: the first build made no binary\n' "$part" "$arch"
			failed=1
		fi
	done
	want=2
fi

if [[ $failed == 0 && $compared != "$want" ]]; then
	echo "build-twice: compared $compared files, expected $want" >&2
	exit 1
fi
if [[ $failed != 0 ]]; then
	echo "build-twice: the two builds of one commit differ" >&2
	exit 1
fi
echo "the two builds of one commit are the same"
