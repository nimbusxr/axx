package demo

import (
	"embed"
	"io/fs"
	"sort"
	"sync"
)

// A module adds to the stage: scenes that draw surfaces of its own, styles,
// and the directions its scenes act on. It is a few files of JavaScript and
// CSS that use the stage as the pack's own scenes do (axxStage.scene,
// axxStage.on; the guide "Build a demo's scenes" says what a scene is given).
// A pack that builds on this one registers its module from its init, and a
// project has its own without a pack: the scripts, styles, surfaces and
// directions of packs.demo in axx.yaml. The pack's terminal is a module like
// any other (modules/terminal).

// Module is what a pack adds to the stage.
type Module struct {
	// Name names it, and is the folder its files are served from.
	Name string
	// Surfaces are the surfaces its scenes draw: what [show ...] can name.
	Surfaces []string
	// Directions are the directions its scenes act on, by name.
	Directions []string
	// Files are its files; Scripts and Styles name those the stage loads,
	// in order, after the pack's own.
	Files   fs.FS
	Scripts []string
	Styles  []string
}

var (
	modulesMu sync.Mutex
	modules   = map[string]Module{}
)

// RegisterModule adds a module to the stage of every demo.
func RegisterModule(m Module) {
	modulesMu.Lock()
	modules[m.Name] = m
	modulesMu.Unlock()
}

// Modules are the registered modules, by name.
func Modules() []Module {
	modulesMu.Lock()
	defer modulesMu.Unlock()
	out := make([]Module, 0, len(modules))
	for _, m := range modules {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

//go:embed modules
var moduleFiles embed.FS

// The pack's own module: a terminal, which shows a demo's steps as someone
// would type them. It is registered as another pack's is.
func init() {
	files, _ := fs.Sub(moduleFiles, "modules/terminal")
	RegisterModule(Module{
		Name: "terminal", Surfaces: []string{"terminal"},
		Files: files, Scripts: []string{"terminal.js"}, Styles: []string{"terminal.css"},
	})
}
