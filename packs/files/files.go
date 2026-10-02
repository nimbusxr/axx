// Package files is the files pack: the files services write to a folder,
// such as an export directory or a volume they share with axx.
package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

const since = "0.1.1"

const packDoc = `Check the files your services write to a folder: an export directory, a volume they share with axx.

Register the folder with ` + "`the {word} folder with the following properties:`" + `: its ` + "`path`" + `, relative to the directory of axx.yaml or absolute (` + "`${env:..}`" + ` and ` + "`${sys:..}`" + ` are expanded). A check names a file by its path in the folder, without spaces, such as ` + "`manifests/M-KESTREL-0412/report.csv`" + `, and waits for it (10 seconds unless ` + "`within {duration}`" + ` says otherwise), since services write asynchronously.

The checks are those of the storage packs' objects: a file's exact content, its JSON properties, its text, read by its type (PDF, Word, Excel, CSV, JSON, XML, HTML or plain text), and a row of its table (CSV, TSV or Excel).

A folder keeps what earlier runs wrote there. Name the files you check after data unique to the scenario, and empty the folder before a run, for example in the Compose file that starts your services.`

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
					Takes: "the folder: a path relative to the directory of axx.yaml, or absolute; `${env:..}` and `${sys:..}` are expanded",
				}},
			},
			Examples: []string{"Given the exports folder with the following properties:\n  | path | ../infra/exports |"},
			Run:      addFolder,
		}}, cloudstep.Objects{
			Pack: "files", Container: "folder", Object: "file", Example: "exports", Since: since,
			Store: func(sc *core.Scenario) (cloudstep.ObjectStore, error) { return store{Folders(sc)}, nil },
		}.Checks()...),
	}
}

// Folder is a folder registered in a scenario.
type Folder struct {
	Name string
	// Path is its absolute path.
	Path string
}

var folders = core.NewStateKey("files", func(*core.Scenario) *core.Services[*Folder] {
	return core.NewServices[*Folder]("Folder",
		`No folder is registered in this scenario; register one with "the {word} folder with the following properties:"`).RegisteredBy("the {word} folder with the following properties:")
}, nil)

// Folders returns the folders registered in a scenario.
func Folders(sc *core.Scenario) *core.Services[*Folder] { return folders.Of(sc) }

// Parse reads a folder's properties, expanding ${env:..} and ${sys:..};
// a relative path is relative to the project directory.
func Parse(s *core.Suite, name string, t *core.Table) (*Folder, error) {
	if t == nil {
		return nil, errors.New(`the folder property "path" is required`)
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	var p string
	for _, pr := range pairs {
		switch pr.Key {
		case "path":
			p = strings.TrimSpace(s.Interpolate(pr.Value))
		default:
			return nil, fmt.Errorf("unknown folder property %q (supported: path)", pr.Key)
		}
	}
	if p == "" {
		return nil, errors.New(`the folder property "path" is required`)
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.ProjectDir(), p)
	}
	return &Folder{Name: name, Path: filepath.Clean(p)}, nil
}

func addFolder(sc *core.Scenario, a core.Args) error {
	f, err := Parse(sc.Suite(), a.String(0), a.Table)
	if err != nil {
		return err
	}
	if err := Folders(sc).Add(f.Name, f); err != nil {
		return err
	}
	if fi, err := os.Stat(f.Path); err != nil || !fi.IsDir() {
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

// file is the path of the named file in a folder.
func (s store) file(folder, name string) (string, error) {
	f, err := s.folders.Get(folder)
	if err != nil {
		return "", fmt.Errorf("no folder named %q in this scenario; register it first with \"the %s folder with the following properties:\"", folder, folder)
	}
	p := filepath.Join(f.Path, filepath.FromSlash(name))
	if rel, err := filepath.Rel(f.Path, p); err != nil || filepath.IsAbs(filepath.FromSlash(name)) ||
		rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not a file in the %s folder: name a file by its path in the folder", name, folder)
	}
	return p, nil
}

func (s store) Put(_ context.Context, folder, name string, body []byte, _ string) error {
	p, err := s.file(folder, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}

func (s store) Get(_ context.Context, folder, name string) ([]byte, bool, error) {
	p, err := s.file(folder, name)
	if err != nil {
		return nil, false, err
	}
	fi, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) || err == nil && !fi.Mode().IsRegular() {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

// List returns the paths of up to max files of a folder and its
// subfolders, leaving out hidden ones (.DS_Store, .gitkeep).
func (s store) List(_ context.Context, folder string, max int) ([]string, error) {
	f, err := s.folders.Get(folder)
	if err != nil {
		return nil, err
	}
	var names []string
	errDone := errors.New("done")
	err = filepath.WalkDir(f.Path, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			if p == f.Path && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipAll // not there yet: no files
			}
			return err
		case p == f.Path:
			return nil
		case strings.HasPrefix(d.Name(), "."):
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case !d.Type().IsRegular():
			return nil
		case len(names) == max:
			return errDone
		}
		rel, err := filepath.Rel(f.Path, p)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !errors.Is(err, errDone) {
		return nil, err
	}
	return names, nil
}
