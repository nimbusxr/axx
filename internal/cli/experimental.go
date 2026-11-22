package cli

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/packref"
	"github.com/nimbusxr/axx/internal/packset"
)

// An experimental pack (core.Manifest.Experimental) may still change, or go, in any release. A
// project adds it as it adds any pack, and axx says what it is where the project meets it: when
// the pack is added, when a run uses it, and when one of its own commands runs. Once for a
// command, on standard error, so that what a command prints for a program stays as it is.

// stabilityPage is the docs' page on what "experimental" means.
const stabilityPage = "/explanations/stability/"

// experimentalNotice says that packs are experimental: "" for none.
func experimentalNotice(packs []string) string {
	switch len(packs) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("the %s pack is experimental: it may change, or go, in any release (%s)", packs[0], axxerr.Docs(stabilityPage))
	}
	return fmt.Sprintf("the %s and %s packs are experimental: they may change, or go, in any release (%s)",
		strings.Join(packs[:len(packs)-1], ", "), packs[len(packs)-1], axxerr.Docs(stabilityPage))
}

// noteExperimental says, once for this command, that packs it uses are experimental.
func (a *App) noteExperimental(packs []string) {
	if len(packs) == 0 || a.notedExperimental {
		return
	}
	a.notedExperimental = true
	fmt.Fprintln(a.Stderr, "axx: "+experimentalNotice(packs))
}

// experimentalPack reports whether a pack is experimental: one compiled into this axx by its
// manifest, and one of axx's that is not, by axx's reference of its packs.
func experimentalPack(name string) bool {
	if pk, ok := engine.Compiled()[name]; ok {
		return pk.Manifest().Experimental
	}
	ref, ok := packref.Lookup(name)
	return ok && ref.Experimental
}

// experimentalAmong are the experimental packs among entries of axx-packs.yaml, with the packs
// of axx's they build on.
func experimentalAmong(raw []string) []string {
	var names []string
	seen := map[string]bool{}
	var add func(name string)
	add = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if p, ok := packset.Lookup(name); ok {
			for _, req := range p.Requires {
				add(req)
			}
		}
		if experimentalPack(name) {
			names = append(names, name)
		}
	}
	for _, r := range raw {
		add(r)
	}
	return names
}
