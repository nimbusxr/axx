package files

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// absences are the checks that a folder no longer has any file. That it no
// longer has one is the object checks' (files.absent).
func absences() []core.StepDef {
	return []core.StepDef{
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
