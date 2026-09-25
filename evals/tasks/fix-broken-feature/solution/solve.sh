#!/usr/bin/env bash
# Reference solution: fix the step text ("db seed", not "database seed") and the
# expectation (rejected lines are REJECTED, not FAILED; docs/manifest-import.md).
set -euo pipefail
f=/app/features/manifest-rejections.feature
sed -i 's/manifest-outcome-valid.yaml database seed/manifest-outcome-valid.yaml db seed/' "$f"
sed -i 's/| status | FAILED                 |/| status | REJECTED               |/' "$f"
grep -q 'manifest-outcome-valid.yaml db seed' "$f"
grep -q '| status | REJECTED ' "$f"
