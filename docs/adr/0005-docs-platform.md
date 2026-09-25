# 0005: Docs platform: Starlight, generated reference, agent-first outputs

- Status: Accepted
- Date: 2026-09-23

## Decision

- Keep Astro Starlight (upgraded); pages are plain Markdown, MDX only where needed.
- The `axx` binary is the single source of truth for reference material: step reference,
  parameter types, CLI, `axx.yaml`, lint rules, error codes and MCP tools are rendered by
  `axx docs export` / `axx schema`. Generators fail on empty output and CI fails on drift.
- Agent outputs: `llms.txt`/`llms-full.txt`, a Markdown twin for every page, agent skills
  generated from the step registry (also embedded in the binary), and an MCP server in the
  binary (`axx mcp`) so agents always see docs matching the installed version.
- Hosted on GitHub Pages at `axx.nimbusxr.us`; schemas under `/schemas/v0/`.
