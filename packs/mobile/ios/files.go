package mobileios

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// Files are the app's files at a path in its sandbox: on a simulator, a
// folder of the Mac's (simctl get_app_container); on a device an Appium
// server of the project's own or a device farm's runs, through Appium.
func (d *running) Files(ctx context.Context, rel string) (core.Files, error) {
	where := "the " + d.app.name + " app's files"
	if rel != "" {
		where += ", at ./" + rel
	}
	if d.dev == nil {
		return mobilecore.RemoteFiles(d.session, "@"+d.bundleID+":data/"+rel, where), nil
	}
	out, err := d.dev.set.simctl(ctx, "get_app_container", d.dev.udid, d.bundleID, "data")
	if err != nil {
		return nil, fmt.Errorf("cannot find the %s app's files on its simulator: %w", d.app.name, err)
	}
	return core.LocalFiles(filepath.Join(strings.TrimSpace(out), filepath.FromSlash(rel))), nil
}
