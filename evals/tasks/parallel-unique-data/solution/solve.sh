#!/usr/bin/env bash
# Reference solution: one seed file per scenario with scenario-specific shops and
# references, the features, and lint rules that keep references and shops unique.
set -euo pipefail
cp -R /solution/files/. /app/

cat >> /app/axx.yaml <<'YAML'

# Test-data isolation: scenarios share one database, so every seed file owns its
# parcels and shops (`axx lint`).
lint:
  config:
    baseDir: .
    mode: error
  rules:
    - name: Parcel references in seeds
      description: A parcel reference belongs to exactly one seed file
      filePatterns: ["seeds/*.yaml"]
      regex: '^\s+-?\s*reference:\s*"([^"]+)"'
      validation: cross-file-unique
    - name: Shops in seeds
      description: A shop belongs to exactly one seed file
      filePatterns: ["seeds/*.yaml"]
      regex: '^\s+-?\s*sender:\s*"([^"]+)"'
      validation: cross-file-unique
YAML

cd /app
axx lint
