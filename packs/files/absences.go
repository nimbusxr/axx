package files

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// absences are the checks that a folder no longer has a file, or any.
func absences() []core.StepDef {
	return []core.StepDef{
		{
			ID: "files.absent", Keyword: "Then", Since: folderSince, Absence: true,
			Expr: "[[within {duration} ]]the {word} folder has no file named {path}",
			Doc: "Check that the folder has no file with that name, or wait for it to go: 10 seconds, or `within {duration}`, " +
				"for a service or an app that deletes or moves it.",
			Examples: []string{"Then the exports folder has no file named manifests/M-KESTREL-0505/report.csv"},
			Run: func(sc *core.Scenario, a core.Args) error {
				st, folder, name, d := store{Folders(sc)}, a.String(1), a.String(2), cloudstep.Wait(a, 0)
				return cloudstep.Poll(sc, d, func() (bool, string, error) {
					_, ok, err := st.Get(sc.Context(), folder, name)
					if err != nil {
						return false, "", fmt.Errorf("cannot read %s from the %s folder: %w", name, folder, err)
					}
					return !ok, fmt.Sprintf("The %s folder still has a file named %s after %s", folder, name, d), nil
				})
			},
		},
		{
			ID: "files.empty", Keyword: "Then", Since: folderSince, Absence: true,
			Expr: "[[within {duration} ]]the {word} folder is empty",
			Doc: "Check that the folder has no files, in its subfolders too, or wait for them to go: 10 seconds, or `within {duration}`. " +
				"Hidden files (.DS_Store, .gitkeep) are not counted, and a folder that is not there is empty.",
			Examples: []string{"Then the exports folder is empty"},
			Run: func(sc *core.Scenario, a core.Args) error {
				st, folder, d := store{Folders(sc)}, a.String(1), cloudstep.Wait(a, 0)
				return cloudstep.Poll(sc, d, func() (bool, string, error) {
					names, err := st.List(sc.Context(), folder, 100)
					if err != nil {
						return false, "", fmt.Errorf("cannot list the %s folder: %w", folder, err)
					}
					return len(names) == 0, fmt.Sprintf("The %s folder is not empty after %s: it has %s", folder, d, cloudstep.Shown("files", names, 20)), nil
				})
			},
		},
	}
}
