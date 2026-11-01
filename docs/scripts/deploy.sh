#!/usr/bin/env bash
# Publishes a built site (the release's docs, with main's at /next/) to the docs'
# S3 bucket behind CloudFront (ADR 0005), from the docs workflow:
#   - every file, with the content types the site needs (text in UTF-8, the
#     install script as a shell script), and what the site no longer has removed;
#   - the pages that moved, into the KeyValueStore the router reads (real 301s);
#   - the router itself (cloudfront/router.js), when its code changed;
#   - then the cache is invalidated.
#   scripts/deploy.sh <site>
# DOCS_BUCKET, DOCS_DISTRIBUTION, DOCS_ROUTER (the function's name) and
# DOCS_MOVED (the KeyValueStore's ARN) say where; AWS credentials come from
# the environment (the workflow's OIDC role).
set -euo pipefail
site=${1:?the built site}
: "${DOCS_BUCKET:?}" "${DOCS_DISTRIBUTION:?}" "${DOCS_ROUTER:?}" "${DOCS_MOVED:?}"
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Every file is copied: a sync would skip a changed file of the same size
# whose copy in the bucket is newer. Text gets its charset.
s3=s3://$DOCS_BUCKET/
aws s3 cp "$site" "$s3" --recursive --only-show-errors \
  --exclude '*.html' --exclude '*.md' --exclude '*.txt' --exclude '*.json'
aws s3 cp "$site" "$s3" --recursive --only-show-errors --exclude '*' --include '*.html' --content-type 'text/html; charset=utf-8'
aws s3 cp "$site" "$s3" --recursive --only-show-errors --exclude '*' --include '*.md' --content-type 'text/markdown; charset=utf-8'
aws s3 cp "$site" "$s3" --recursive --only-show-errors --exclude '*' --include '*.txt' --content-type 'text/plain; charset=utf-8'
aws s3 cp "$site" "$s3" --recursive --only-show-errors --exclude '*' --include '*.json' --content-type 'application/json; charset=utf-8'
aws s3 sync "$site" "$s3" --delete --size-only --only-show-errors

# The pages that moved: the store's keys are brought to the site's, in
# batches of at most 50 changes (UpdateKeys' limit), each with the store's
# latest ETag.
node "$here/redirects.mjs" "$site" > "$work/moved.json"
aws cloudfront-keyvaluestore list-keys --kvs-arn "$DOCS_MOVED" --output json > "$work/keys.json"
node - "$work" <<'EOF'
const fs = require('node:fs');
const dir = process.argv[2];
const want = JSON.parse(fs.readFileSync(`${dir}/moved.json`, 'utf8'));
const have = Object.fromEntries((JSON.parse(fs.readFileSync(`${dir}/keys.json`, 'utf8')).Items || []).map((i) => [i.Key, i.Value]));
const changes = [
	...Object.entries(want).filter(([k, v]) => have[k] !== v).map(([Key, Value]) => ({ put: { Key, Value } })),
	...Object.keys(have).filter((k) => !(k in want)).map((Key) => ({ del: { Key } })),
];
for (let i = 0, n = 0; i < changes.length; i += 50, n++) {
	const batch = changes.slice(i, i + 50);
	const puts = batch.filter((c) => c.put).map((c) => c.put);
	const dels = batch.filter((c) => c.del).map((c) => c.del);
	const name = String(n).padStart(4, '0');
	if (puts.length) fs.writeFileSync(`${dir}/batch-${name}.puts.json`, JSON.stringify(puts));
	if (dels.length) fs.writeFileSync(`${dir}/batch-${name}.deletes.json`, JSON.stringify(dels));
	fs.writeFileSync(`${dir}/batch-${name}`, '');
}
console.log(`moved pages: ${Object.keys(want).length}, ${changes.length} change(s) to the store`);
EOF
for b in "$work"/batch-????; do
  [ -e "$b" ] || break
  etag=$(aws cloudfront-keyvaluestore describe-key-value-store --kvs-arn "$DOCS_MOVED" --query ETag --output text)
  args=(--kvs-arn "$DOCS_MOVED" --if-match "$etag")
  [ -e "$b.puts.json" ] && args+=(--puts "file://$b.puts.json")
  [ -e "$b.deletes.json" ] && args+=(--deletes "file://$b.deletes.json")
  aws cloudfront-keyvaluestore update-keys "${args[@]}" > /dev/null
done

# The router, when the repository's differs from the one running.
aws cloudfront get-function --name "$DOCS_ROUTER" --stage LIVE "$work/live.js" > /dev/null
if ! cmp -s "$here/../cloudfront/router.js" "$work/live.js"; then
  etag=$(aws cloudfront describe-function --name "$DOCS_ROUTER" --query ETag --output text)
  aws cloudfront update-function --name "$DOCS_ROUTER" --if-match "$etag" \
    --function-config "{\"Comment\":\"The docs site's router (docs/cloudfront/router.js)\",\"Runtime\":\"cloudfront-js-2.0\",\"KeyValueStoreAssociations\":{\"Quantity\":1,\"Items\":[{\"KeyValueStoreARN\":\"$DOCS_MOVED\"}]}}" \
    --function-code "fileb://$here/../cloudfront/router.js" > /dev/null
  etag=$(aws cloudfront describe-function --name "$DOCS_ROUTER" --query ETag --output text)
  aws cloudfront publish-function --name "$DOCS_ROUTER" --if-match "$etag" > /dev/null
  echo "router: published"
fi

id=$(aws cloudfront create-invalidation --distribution-id "$DOCS_DISTRIBUTION" --paths '/*' --query Invalidation.Id --output text)
aws cloudfront wait invalidation-completed --distribution-id "$DOCS_DISTRIBUTION" --id "$id"
echo "deployed: $(find "$site" -type f | wc -l | tr -d ' ') files"
