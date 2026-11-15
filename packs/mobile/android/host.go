package mobileandroid

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// The pack's side in the axx host (core.HostServer): a run in an agent's
// sandbox cannot start an emulator (it runs its virtual machine on the
// system's hypervisor, which the sandbox keeps from it), so the host starts
// the emulator and adb's server, installs the driver and the app, and does
// the rest that is the machine's, with this pack's own code
// (mobilecore.ServeHost).

// HostServes says what the pack does in the host, for the person who starts
// it.
func (pack) HostServes() string {
	return "the emulators and devices of Android scenarios, and their apps (each app once you approved it)"
}

// ServeHost serves a run's Android apps until its connection ends.
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	return mobilecore.ServeHost(ctx, run, conn, func(sc *core.Scenario, name string, t *core.Table) error {
		return registerApp(sc, name, t, func(v string) string { return v })
	})
}

// HostApproval is what the person who runs the host approves before it
// starts the app: the app, the device, what the registration puts on it and
// lets it reach, and the arguments the project's settings give the
// emulator, each a line. The app runs in the emulator, a virtual machine,
// and not as a process of the person's machine; it reaches the network from
// there, which the warnings say.
func (r runner) HostApproval(sc *core.Scenario) core.HostApproval {
	a := r.app
	project := sc.Suite().ProjectDir()
	file := func(path string) string {
		if rel, err := filepath.Rel(project, path); err == nil && filepath.IsLocal(rel) {
			return filepath.ToSlash(rel)
		}
		return path
	}
	files := func(paths []string) string {
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = file(p)
		}
		return strings.Join(out, ", ")
	}
	var lines []string
	line := func(name, value string) {
		if value != "" {
			lines = append(lines, name+": "+value)
		}
	}
	if a.apk != "" {
		line("apk", file(a.apk))
	}
	line("package", a.pkg)
	line("activity", a.activity)
	line("device", a.device)
	line("permissions", strings.Join(a.permissions, ", "))
	ports := make([]string, len(a.hostPorts))
	for i, p := range a.hostPorts {
		ports[i] = strconv.Itoa(p)
	}
	line("host ports", strings.Join(ports, ", "))
	line("photos", files(a.photos))
	line("documents", files(a.documents))
	if s, err := settingsFor(sc.Suite()); err == nil {
		// The project's own arguments for the emulator the host starts.
		line("emulator arguments", strings.Join(s.emulatorArgs, " "))
	}
	warnings := []string{
		"the app is the code of the agent that builds it: on an emulator it runs in a virtual machine, not as a process of this machine, and reaches the network from there, past the agent's sandbox",
	}
	if len(a.hostPorts) > 0 {
		warnings = append(warnings, "the app reaches this machine's ports "+strings.Join(ports, ", ")+" as its own localhost")
	}
	warnings = append(warnings, "the approval is for the app at this path, whatever is built there from now on")
	return core.HostApproval{
		Subject:  fmt.Sprintf("the %s app, on Android", a.name),
		Text:     strings.Join(lines, "\n"),
		Warnings: warnings,
	}
}
