#!/usr/bin/env bash
# Fails if the tree contains references to unrelated organizations, internal
# infrastructure, old registries or unrelated projects, or if gitleaks finds
# secrets.
# Allowed exceptions live in scripts/hygiene-allow.txt as "path:regex" lines.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Assembled from parts so this script does not match itself.
parts=('ko''hl' '(^|[^a-z0-9])k''pp([^a-z0-9]|$)' 'par''lor' 'vault-''internal' '10360''152' 'projects/0000''000' 'gitlab\.''com' 'gitlab\.''io' '(^|[^a-z0-9])a''ces')
pattern=$(IFS='|'; echo "${parts[*]}")

allow_file=scripts/hygiene-allow.txt
hits=$(git ls-files -z --cached --others --exclude-standard \
  | xargs -0 grep -InEi "$pattern" -- 2>/dev/null || true)

if [[ -n "$hits" && -f "$allow_file" ]]; then
  while IFS= read -r rule; do
    [[ -z "$rule" || "$rule" == \#* ]] && continue
    path=${rule%%:*}; re=${rule#*:}
    hits=$(printf '%s\n' "$hits" | grep -vE "^${path}:[0-9]+:.*${re}" || true)
  done < "$allow_file"
fi

status=0
if [[ -n "$hits" ]]; then
  echo "hygiene: forbidden references found:" >&2
  printf '%s\n' "$hits" >&2
  status=1
fi

if command -v gitleaks >/dev/null 2>&1; then
  gitleaks dir --no-banner --redact . || status=1
else
  echo "hygiene: gitleaks not installed; skipping secret scan (CI runs it)" >&2
fi

exit $status
