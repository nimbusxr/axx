package mobileios

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// The pack's side in the axx host (core.HostServer): a run in an agent's
// sandbox cannot reach the simulators (simctl talks to CoreSimulator's
// service, which the sandbox keeps from it), so the host leases the
// simulator, installs and starts the app, and does the rest that is the
// Mac's, with this pack's own code (mobilecore.ServeHost).

// HostServes says what the pack does in the host, for the person who starts
// it.
func (pack) HostServes() string {
	return "the simulators of iOS scenarios, and their apps (each app once you approved it)"
}

// ServeHost serves a run's iOS apps until its connection ends.
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return mobilecore.ServeHost(ctx, run, conn, func(sc *core.Scenario, name string, t *core.Table) error {
		return registerApp(sc, name, t, func(v string) string { return v })
	})
}

// HostApproval is what the person who runs the host approves before it
// starts the app: the app, the simulator, and what the registration puts on
// it, each a line. An app in the iOS Simulator is a process of the Mac's, so
// the warning says what it reaches.
func (r runner) HostApproval(sc *core.Scenario) core.HostApproval {
	a := r.app
	project := sc.Suite().ProjectDir()
	file := func(path string) string {
		if rel, err := filepath.Rel(project, path); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
		return path
	}
	var lines []string
	line := func(name, value string) {
		if value != "" {
			lines = append(lines, name+": "+value)
		}
	}
	if a.path != "" {
		line("app", file(a.path))
	}
	line("bundle id", a.bundleID)
	line("simulator", a.device)
	line("permissions", strings.Join(a.permissions, ", "))
	files := func(paths []string) string {
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = file(p)
		}
		return strings.Join(out, ", ")
	}
	line("photos", files(a.photos))
	line("documents", files(a.documents))
	return core.HostApproval{
		Subject: fmt.Sprintf("the %s app, in the iOS Simulator", a.name),
		Text:    strings.Join(lines, "\n"),
		Warnings: []string{
			"an app in the iOS Simulator is a process of this Mac, as you: it reads and writes your files and reaches the network, past the sandbox of the agent that builds it",
			"the approval is for the app at this path, whatever is built there from now on",
		},
	}
}
