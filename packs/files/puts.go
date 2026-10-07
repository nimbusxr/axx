package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// puts are the steps that put files in a folder, and empty it.
func puts() []core.StepDef {
	return []core.StepDef{
		{
			ID: "files.copy", Keyword: "Given", Since: folderSince,
			Expr: "the {path} file in the {word} folder is a copy of the {filepath} file",
			Doc: "Put a copy of a file of the project in the folder, under the path given, as a service or an app would find it there; " +
				"the folders it is in are made, and a file there is replaced.",
			Examples: []string{"Given the manifests/M-KESTREL-0505/report.csv file in the exports folder is a copy of the reports/M-KESTREL-0505-earlier.csv file"},
			Run: func(sc *core.Scenario, a core.Args) error {
				p, err := sc.Suite().ResolvePath(a.String(2))
				if err != nil {
					return err
				}
				body, err := os.ReadFile(p)
				if err != nil {
					return fmt.Errorf("cannot read %s: %w", a.String(2), err)
				}
				return put(sc, a.String(1), a.String(0), body)
			},
		},
		{
			ID: "files.content", Keyword: "Given", Arg: core.ArgDocString, Since: folderSince,
			Expr: "the {path} file in the {word} folder has the content:",
			Doc: "Put a file with the doc string's content in the folder, as a service or an app would find it there; " +
				"the folders it is in are made, and a file there is replaced. `${env:..}` and `${sys:..}` are expanded.",
			Examples: []string{"Given the manifests/M-KESTREL-0505/summary.json file in the exports folder has the content:\n  \"\"\"json\n  {\"manifest\": \"M-KESTREL-0505\", \"lines\": 1}\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				if a.DocString == nil {
					return fmt.Errorf("the %s file's content is the step's doc string: put it under the step, between \"\"\" lines", a.String(0))
				}
				return put(sc, a.String(1), a.String(0), []byte(sc.Suite().Interpolate(a.DocString.Content)))
			},
		},
		{
			ID: "files.emptied", Keyword: "Given", Since: folderSince,
			Expr: "the {word} folder is emptied",
			Doc: "Remove everything in the folder, hidden files and subfolders too, and keep the folder: a folder of the project, " +
				"a service's or an app's. A folder elsewhere on this machine is a person's, and is not emptied.",
			Examples: []string{"Given the kestrel folder is emptied"},
			Run: func(sc *core.Scenario, a core.Args) error {
				f, err := (store{Folders(sc)}).folder(a.String(0))
				if err != nil {
					return err
				}
				if !f.Owned && f.Path != "" {
					project := sc.Suite().ProjectDir()
					rel, err := filepath.Rel(project, f.Path)
					if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
						return fmt.Errorf("the %s folder (%s) is not emptied: it is not in the project (%s), and a folder elsewhere on this machine is a person's; "+
							"name a folder of the project, or one a service or an app of the scenario owns", f.Name, f.Path, project)
					}
				}
				if err := f.Files.Empty(sc.Context()); err != nil {
					return fmt.Errorf("cannot empty the %s folder (%s): %w", f.Name, f.Files.Where(), err)
				}
				sc.Log("emptied the %s folder (%s)", f.Name, f.Files.Where())
				return nil
			},
		},
	}
}

// folderSince is the version the steps that put files in a folder, empty it
// and check what went from it came with.
const folderSince = "0.2.3"

// put writes a file in the folder.
func put(sc *core.Scenario, folder, name string, body []byte) error {
	st := store{Folders(sc)}
	if err := st.Put(sc.Context(), folder, name, body, ""); err != nil {
		return fmt.Errorf("cannot put %s in the %s folder: %w", name, folder, err)
	}
	sc.Log("put %s in the %s folder (%d bytes)", name, folder, len(body))
	return nil
}
