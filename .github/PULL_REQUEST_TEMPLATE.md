## What & why

<!-- One or two sentences. Link the issue: "Fixes #123". -->

## Checklist

- [ ] PR title is a conventional commit (`feat(rest): …`, `fix: …`, `docs: …`)
- [ ] Tests added/updated (goldens refreshed with `go test ./... -update` if output changed)
- [ ] `mise run generate` run; generated files committed
- [ ] No change to existing step text, exit codes, `--json` shape or `axx.yaml` schema, **or** an ADR is included
- [ ] Commits are signed off (`git commit -s`, DCO)
- [ ] AI-assisted (a human reviewed every line)
