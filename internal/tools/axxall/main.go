// Command axxall is axx built with every pack axx publishes. The repository
// uses it for what covers all of them: the generated step reference, the
// skills, and checking the steps in the documentation. Projects never use
// it: axx builds itself with the packs a project lists.
package main

import (
	"github.com/nimbusxr/axx/app"
	"github.com/nimbusxr/axx/packs/all"
)

func main() { app.Main(all.Packs()) }
