package desktopmacos

import (
	"fmt"
	"strings"

	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// preferenceDomains are the preferences domains the registration names,
// separated by commas. A domain is a name, like com.parcels-example.Depot
// desk: never a file, nor the global domain every app shares, which a reset
// would empty for every app.
func preferenceDomains(app *desktopcore.App) ([]string, error) {
	var domains []string
	for _, d := range strings.Split(app.Extra["preferences"], ",") {
		d = strings.TrimSpace(d)
		switch {
		case d == "":
			continue
		case strings.ContainsAny(d, "/~") || strings.HasPrefix(d, "-") || strings.EqualFold(d, "NSGlobalDomain") || strings.HasPrefix(d, "."):
			return nil, fmt.Errorf("the %s macos app's preferences: %q is not a preferences domain's name, like com.parcels-example.Depot desk "+
				"(a file, and the global domain every app shares, are not emptied)", app.Name, d)
		}
		domains = append(domains, d)
	}
	return domains, nil
}
