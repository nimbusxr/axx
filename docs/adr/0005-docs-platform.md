# 0005: Docs platform: Starlight, generated reference, agent-first outputs

- Status: Accepted; amended 2026-11-01 (hosting) and 2026-11-11 (a copy for networks that block the site)
- Date: 2026-09-23

## Decision

- Keep Astro Starlight (upgraded); pages are plain Markdown, MDX only where needed.
- The `axx` binary is the single source of truth for reference material: step reference,
  parameter types, CLI, `axx.yaml`, lint rules, error codes and MCP tools are rendered by
  `axx docs export` / `axx schema`. Generators fail on empty output and CI fails on drift.
- Agent outputs: `llms.txt`/`llms-full.txt`, a Markdown twin for every page, agent skills
  generated from the step registry (also embedded in the binary), and an MCP server in the
  binary (`axx mcp`) so agents always see docs matching the installed version.
- Hosted at `axx.nimbusxr.us`, from an S3 bucket behind CloudFront; schemas under `/schemas/v0/`.
  What people's machines fetch while they work comes from GitHub, which more networks allow than
  a site of our own: the install scripts and the schemas (`schemas/v0`), which axx.yaml files name
  by their addresses on raw.githubusercontent.com (GitHub's address for a release's latest files
  skips pre-releases, and every 0.x release is one). The site keeps serving its copies for the links
  already out there.
  The docs workflow publishes the site (`docs/scripts/deploy.sh`), through a role its OIDC
  token assumes on `main` only.
- A page's address answers in the format the request prefers: a browser gets the page, and a
  request that ranks `text/markdown` first (coding agents' fetchers do) its Markdown twin. A
  CloudFront function (`docs/cloudfront/router.js`) chooses, and sends a real 301 for a page
  that moved, from a KeyValueStore the deploy keeps.

## Amendment, 2026-11-01: from GitHub Pages to CloudFront

GitHub Pages serves one file per address, whatever a request asks for: agents that fetched a
page read its HTML, the sidebar included (199 KB for a pack's reference against its 10 KB
twin), and a moved page was a page that refreshes, with a 200, not a redirect. Content
negotiation, real redirects and headers (`Vary: Accept`) need a server that chooses, and an
S3 bucket keeps earlier releases' docs too, if the site ever serves them.

## Amendment, 2026-11-11: a copy for networks that block the site

Some organizations filter `axx.nimbusxr.us`, and some machines reach no network. Every release
publishes its docs beside its binaries, `axx_<version>_docs.tar.gz` in its `checksums.txt`, built
from the tag for the root of a host (`AXX_DOCS_CHANNEL=archive`). `axx docs serve` serves the docs
of the axx that runs on 127.0.0.1 (port 7357 unless `--port` or `docs.port` names another; never
another port by itself), downloaded once from the release, or its mirror, into axx's cache; an
organization hosts the same archive on its own server. Nothing on a page loads from another host
(`npm run check:offline`, on every build), so a copy works offline, search included. The links
axx prints lead to the copy that `AXX_DOCS_URL`, or axx.yaml's `docs.url`, names.

The local server answers as the router does (`internal/docsite`): Markdown twins, real 301s for
moved pages, which it reads from the pages Astro writes for them, and the 404 page. Changing one
means changing the other. The archive is not embedded in the binary: the core stays small, and most
people never read the docs offline.
