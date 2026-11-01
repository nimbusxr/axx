# 0005: Docs platform: Starlight, generated reference, agent-first outputs

- Status: Accepted; amended 2026-11-01 (hosting)
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
