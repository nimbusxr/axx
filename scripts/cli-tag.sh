#!/usr/bin/env sh
# SPDX-License-Identifier: Apache-2.0
# Prints the CLI's newest release tag (v0.1.0...), or v0.0.0 before the first release. The
# WireMock extension and the IntelliJ plugin are tagged on the same commits
# (wiremock-openapi-v..., intellij-v...), so GoReleaser must not guess from `git describe`:
#
#   GORELEASER_CURRENT_TAG=$(scripts/cli-tag.sh) goreleaser release --snapshot --clean
git describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null || echo v0.0.0
