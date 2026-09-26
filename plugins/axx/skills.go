// Package axx is axx's plugin for Claude Code. Its skills, generated for
// every pack axx publishes, are also what `axx skills install --scope user`
// installs: a home directory serves every project, whatever its packs.
package axx

import "embed"

// Skills holds skills/<skill>/..., as `go generate ./internal/skills` wrote them.
//
//go:embed skills
var Skills embed.FS
