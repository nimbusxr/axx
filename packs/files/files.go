// Package files is the files pack: the files services and apps write to a
// folder, such as an export directory, a volume they share with axx, or an
// app's own data.
package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

const since = "0.1.1"

const packDoc = `Check the files your services and apps write to a folder: an export directory, a volume they share with axx, an app's own data.

Register the folder with ` + "`the {word} folder with the following properties:`" + `. Its ` + "`path`" + ` is on this machine, relative to the directory of axx.yaml or absolute, or in the file context of its ` + "`owner`" + `: ` + "`service:parcels`" + ` for a service of axx.yaml (relative to the folder axx runs it in), ` + "`app:depot`" + ` for an app a scenario registers (` + "`./`" + ` is where the app keeps its data, ` + "`~/`" + ` its home, wherever it runs). ` + "`${env:..}`" + ` and ` + "`${sys:..}`" + ` are expanded. A check names a file by its path in the folder, such as ` + "`manifests/M-KESTREL-0412/report.csv`" + `, quoted when it has a space, and waits for it (10 seconds unless ` + "`within {duration}`" + ` says otherwise), since services write asynchronously.

The checks are those of the storage packs' objects: a file's exact content, its JSON properties, its text, read by its type (PDF, Word, Excel, CSV, JSON, XML, HTML or plain text), and a row of its table (CSV, TSV or Excel). What a service or an app takes away is checked too: a folder ` + "`has no file named`" + ` one, or ` + "`is empty`" + `, which wait for the files to go. An image an app saves is compared with its screenshot, pixel by pixel, one for each platform: ` + "`packs.files.screenshots`" + ` sets their ` + "`folder`" + `, ` + "`tolerance`" + `, ` + "`update`" + ` and ` + "`platforms`" + `, as the app screenshots' settings do.

A scenario puts files in a folder too, as a service or an app would find them: ` + "`is a copy of`" + ` a file of the project, or ` + "`has the content:`" + ` of a doc string. ` + "`the {word} folder is emptied`" + ` removes what is in it, hidden files and subfolders too: a folder of the project, a service's or an app's, never one elsewhere on the machine.

A service's folder keeps what earlier runs wrote there, and axx empties no folder unless a scenario says so: whether a check needs a file of its scenario's own (named after data unique to it), or a folder emptied first, depends on the test. An app's files are its own in each scenario.`

// Pack returns the files pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "files",
		Namespace: "files",
		Doc:       packDoc,
		Steps: append([]core.StepDef{{
			ID: "files.folder", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} folder with the following properties:",
			Doc:  "Register a folder under a name. The folder may appear only when a service first writes to it.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{{
					Name: "path", Required: true,
					Takes: "the folder: with no `owner`, a path relative to the directory of axx.yaml, or absolute; with one, a path in its file context (`./exports`); `${env:..}` and `${sys:..}` are expanded",
				}, {
					Name:    "owner",
					Takes:   "whose folder it is: `service:<name>`, a service of axx.yaml, whose paths are relative to the folder axx runs it in; or `app:<name>`, an app the scenario registers, where `./` is where the app keeps its data and `~/` its home",
					Default: "none: a folder on this machine",
				}},
			},
			Examples: []string{
				"Given the exports folder with the following properties:\n  | owner | service:parcels |\n  | path  | ./exports       |",
				"Given the desk folder with the following properties:\n  | owner | app:depot              |\n  | path  | \"./Parcels/Depot desk\" |",
			},
			Run: addFolder,
		}}, append(append(append(puts(), objects.Checks()...), absences()...), screenshotStep(objects))...),
		ConfigSchema: []byte(configSchema),
	}
}

var objects = cloudstep.Objects{
	Pack: "files", Container: "folder", Object: "file", Example: "exports", Since: since,
	Store: func(sc *core.Scenario) (cloudstep.ObjectStore, error) { return store{Folders(sc)}, nil },
}

// Folder is a folder registered in a scenario.
type Folder struct {
	Name string
	// Files are its files, wherever they are.
	Files core.Files
	// Path is its absolute path, when it is a folder on this machine.
	Path string
	// Owned is whether it is a service's or an app's, not a folder of the
	// machine's.
	Owned bool
}

var folders = core.NewStateKey("files", func(*core.Scenario) *core.Services[*Folder] {
	return core.NewServices[*Folder]("Folder",
		`No folder is registered in this scenario; register one with "the {word} folder with the following properties:"`).RegisteredBy("the {word} folder with the following properties:")
}, nil)

// Folders returns the folders registered in a scenario.
func Folders(sc *core.Scenario) *core.Services[*Folder] { return folders.Of(sc) }

// Parse reads a folder's properties, expanding ${env:..} and ${sys:..}. A
// folder with no owner is on this machine, its path relative to the project
// directory; one with an owner is in the owner's file context.
func Parse(sc *core.Scenario, name string, t *core.Table) (*Folder, error) {
	if t == nil {
		return nil, errors.New(`the folder property "path" is required`)
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	s := sc.Suite()
	var p, owner string
	for _, pr := range pairs {
		switch pr.Key {
		case "path":
			p = unquote(strings.TrimSpace(s.Interpolate(pr.Value)))
		case "owner":
			owner = strings.TrimSpace(s.Interpolate(pr.Value))
		default:
			return nil, fmt.Errorf("unknown folder property %q (supported: path, owner)", pr.Key)
		}
	}
	if p == "" {
		return nil, errors.New(`the folder property "path" is required`)
	}
	if owner == "" {
		return local(name, p, s.ProjectDir()), nil
	}
	kind, who, ok := strings.Cut(owner, ":")
	switch {
	case !ok || who == "" || kind != "service" && kind != "app":
		return nil, fmt.Errorf(`the folder's owner %q is not "service:<name>" (a service of axx.yaml) or "app:<name>" (an app the scenario registers)`, owner)
	case kind == "service":
		d, ok := s.DeclaredService(who)
		if !ok {
			var names []string
			for _, d := range s.DeclaredServices() {
				names = append(names, d.Name)
			}
			declared := "none"
			if len(names) > 0 {
				declared = strings.Join(names, ", ")
			}
			return nil, fmt.Errorf("the folder's owner %q is no service of axx.yaml (its services: %s)", owner, declared)
		}
		f := local(name, p, d.Dir)
		f.Owned = true
		return f, nil
	}
	files, ok := s.FileOwner(kind)
	if !ok {
		return nil, fmt.Errorf("the folder's owner %q is an app, and no pack of the run runs apps: add the pack of the platform it runs on (mobile-android, mobile-ios, desktop-macos, desktop-windows, desktop-linux)", owner)
	}
	f, err := files(sc, who, p)
	if err != nil {
		return nil, err
	}
	return &Folder{Name: name, Files: f, Owned: true}, nil
}

// local is a folder on this machine: p, relative to dir or absolute.
func local(name, p, dir string) *Folder {
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	p = filepath.Clean(p)
	return &Folder{Name: name, Files: core.LocalFiles(p), Path: p}
}

// unquote takes the quotes off a path quoted for its spaces.
func unquote(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		return p[1 : len(p)-1]
	}
	return p
}

func addFolder(sc *core.Scenario, a core.Args) error {
	f, err := Parse(sc, a.String(0), a.Table)
	if err != nil {
		return err
	}
	if err := Folders(sc).Add(f.Name, f); err != nil {
		return err
	}
	if f.Path == "" {
		sc.Log("the %s folder is %s", f.Name, f.Files.Where())
	} else if fi, err := os.Stat(f.Path); err != nil || !fi.IsDir() {
		sc.Log("the %s folder is %s, which is not there yet", f.Name, f.Path)
	} else {
		sc.Log("the %s folder is %s", f.Name, f.Path)
	}
	return nil
}

// store is the scenario's folders, as the object checks read them: a
// folder is a container, a file an object named by its path in the
// folder.
type store struct {
	folders *core.Services[*Folder]
}

func (s store) folder(name string) (*Folder, error) {
	f, err := s.folders.Get(name)
	if err != nil {
		return nil, fmt.Errorf("no folder named %q in this scenario; register it first with \"the %s folder with the following properties:\"", name, name)
	}
	return f, nil
}

// inFolder refuses a file name that is not a path in the folder.
func inFolder(folder, name string) error {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || path.IsAbs(clean) || filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%s is not a file in the %s folder: name a file by its path in the folder", name, folder)
	}
	return nil
}

func (s store) Put(ctx context.Context, folder, name string, body []byte, _ string) error {
	f, err := s.folder(folder)
	if err != nil {
		return err
	}
	if err := inFolder(folder, name); err != nil {
		return err
	}
	return f.Files.Write(ctx, name, body)
}

func (s store) Get(ctx context.Context, folder, name string) ([]byte, bool, error) {
	f, err := s.folder(folder)
	if err != nil {
		return nil, false, err
	}
	if err := inFolder(folder, name); err != nil {
		return nil, false, err
	}
	return f.Files.Read(ctx, name)
}

// List returns the paths of up to max files of a folder and its
// subfolders, leaving out hidden ones (.DS_Store, .gitkeep).
func (s store) List(ctx context.Context, folder string, max int) ([]string, error) {
	f, err := s.folder(folder)
	if err != nil {
		return nil, err
	}
	return f.Files.List(ctx, max)
}
