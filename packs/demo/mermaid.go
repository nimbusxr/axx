package demo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/npm"
)

// A project's diagrams are written in Mermaid, the language of flowcharts
// that documentation sites and GitHub draw. The stage reads them with
// Mermaid's own parser, so that a diagram written for a README is read here
// as it is there, and draws them itself, in characters (stage/diagram.js):
// nothing of Mermaid's drawing is used.
//
// mermaidVersion is the Mermaid whose parser the stage loads (MIT, Knut
// Sveidqvist and contributors): downloaded from npm the first time a project
// with diagrams makes a demo, never distributed with axx, and checked against
// mermaidIntegrity, npm's integrity of its package. To upgrade it, change both
// (`npm view mermaid@<version> dist.integrity`) and run the pack's browser
// tests: the stage reads the parser's flowchart (its vertices, edges and
// subgraphs), which is Mermaid's own to change.
const (
	mermaidVersion   = "12.3.0"
	mermaidIntegrity = "sha512-4113TdXeLAsMruJ79OpFL5O8+LTFXhQqZ260rF3tgUmiNAtRm4Uume04I+65PRN6yZ69J4f8rVYWW8zRk65UYw=="
)

// mermaidFile is Mermaid's library in axx's cache, downloaded once from the
// npm registry (npm_config_registry points it at a mirror).
func mermaidFile(ctx context.Context, downloading func()) (string, error) {
	dest := filepath.Join(npm.CacheDir("demo"), "mermaid-"+mermaidVersion, "mermaid.min.js")
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	if downloading != nil {
		downloading()
	}
	body, err := npm.Fetch(ctx, fmt.Sprintf("%s/mermaid/-/mermaid-%s.tgz", npm.Registry(), mermaidVersion))
	if err != nil {
		return "", fmt.Errorf("cannot download Mermaid %s, which reads the project's diagrams: %w\n  npm_config_registry points the download at a mirror of npm's registry", mermaidVersion, err)
	}
	if got := npm.Integrity(body); got != mermaidIntegrity {
		return "", fmt.Errorf("mermaid %s: integrity %s does not match the pinned %s", mermaidVersion, got, mermaidIntegrity)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Whole or not at all: another axx may be unpacking it too.
	part := fmt.Sprintf("%s.%d.part", dest, os.Getpid())
	if err := npm.UnpackFile(body, "package/dist/mermaid.min.js", part); err != nil {
		return "", fmt.Errorf("mermaid %s: %w", mermaidVersion, err)
	}
	if err := os.Rename(part, dest); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	return dest, nil
}

// Diagram is one of a project's diagrams: its name and its Mermaid text.
type Diagram struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// diagramsOf reads the project's diagrams, in the order axx.yaml lists them.
func diagramsOf(projectDir string, st Settings) ([]Diagram, error) {
	var out []Diagram
	for _, name := range st.Diagrams.Names {
		file := st.Diagrams.Files[name]
		src, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(file)))
		if err != nil {
			return nil, fmt.Errorf("cannot read the diagram %s of packs.demo.diagrams: %w", name, err)
		}
		out = append(out, Diagram{Name: name, Source: string(src)})
	}
	return out, nil
}
