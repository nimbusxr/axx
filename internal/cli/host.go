package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/host"
	"github.com/nimbusxr/axx/internal/version"
)

func newHostCmd(app *App) *cobra.Command {
	var cf configFlags
	var ide bool
	var addr string
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Start the services for runs that cannot start them, like an agent's in a sandbox without Docker",
		Long: `Serve the project's runs that cannot start its services themselves: an agent's,
in a sandbox that keeps it from Docker. Start it in the project, on your own
machine, and leave it running. It listens on a loopback port and writes
.axx/host.json, which axx finds there in the sandbox: each ` + "`axx run`" + `, ` + "`axx up`" + `
and MCP session asks the host for its services. The host cleans up, starts them as
axx.yaml says, and stops them and runs their cleanup when the run ends, or when its
connection does. The scenarios run where axx runs: the sandbox's rules apply to them.
Runs take turns with the services.

Only a definition you approved starts: axx.yaml's services, what each compose
project they run resolves to, and the files their commands run. Starting the host
approves it as it is, unless it changed since you last approved one: then it asks
you as it starts. When the definition changes, the host asks you here, with the
difference and what in it reaches past a container (privileged containers, this
machine's files, the Docker socket, its network). A run waits a minute for your y
or n; the question stays open after, and the next run has your answer. A change you
refuse is refused again until the host restarts. Approvals are kept in your
configuration directory, outside the project.

With --ide, an editor that started the host asks instead: the host writes its
events as lines of JSON and reads the editor's answers on its standard input.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			if len(cfg.Services) == 0 {
				return axxerr.New("AXX-E0001", exitcode.Usage, "axx.yaml declares no services: an axx host has nothing to start").
					WithHint("declare the services runs start under services: in %s", relPath(filepath.Join(cfg.Dir, "axx.yaml")))
			}
			props, err := cf.properties()
			if err != nil {
				return err
			}
			// The host stops and cleans up when its terminal closes (SIGHUP),
			// as on Ctrl-C. When what reads its output is gone (an editor
			// that quit), a write fails instead of killing it (SIGPIPE): it
			// stops when its input ends, and cleans up.
			ctx, stopOnHangup := signal.NotifyContext(cmd.Context(), syscall.SIGHUP)
			defer stopOnHangup()
			signal.Ignore(syscall.SIGPIPE)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			var events func(host.Event)
			var approver host.Approver
			if ide {
				var mu sync.Mutex
				enc := json.NewEncoder(app.Stdout)
				events = func(e host.Event) {
					mu.Lock()
					defer mu.Unlock()
					_ = enc.Encode(e)
				}
				// The editor that started the host answers; when it is gone,
				// so is the host.
				approver = host.NewIDE(os.Stdin, events, cancel)
			} else {
				t := host.NewTerminal(os.Stdin, app.Stdout, app.color(), stdinIsTerminal())
				events, approver = t.Event, t
			}
			srv, err := host.New(ctx, host.Options{
				Load:     config.LoadOptions{Path: app.Config, Profile: cf.profile, Properties: props, Settings: cf.settings},
				Addr:     addr,
				Approver: approver,
				Events:   events,
				Report:   ide,
				Version:  version.Get().Version,
				Logger:   app.logger(),
			})
			if err != nil {
				return err
			}
			return srv.Serve(ctx)
		},
	}
	cf.register(cmd)
	cmd.Flags().BoolVar(&ide, "ide", false, "talk to the editor that started the host: events as JSON lines on stdout, its answers on stdin")
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:0", "the address to listen on (port 0: one the system picks)")
	return cmd
}

// stdinIsTerminal reports whether a person types into standard input
// (/dev/null is a character device too).
func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// hostFor is the axx host serving the project of cfg, when its runs should
// have their services from one: not when `axx up` keeps them running here.
func (a *App) hostFor(cfg *config.Config) (*host.Announcement, error) {
	if _, up := liveUp(cfg); up {
		return nil, nil
	}
	return host.Find(cfg.Dir)
}

// hostRequest is what a run asks a host for: the services it needs, from
// the configuration it loaded.
func (a *App) hostRequest(cfg *config.Config, cf configFlags, services []string) (host.Request, error) {
	props, err := cf.properties()
	if err != nil {
		return host.Request{}, err
	}
	profile := cf.profile
	if profile == "" {
		profile = os.Getenv("AXX_PROFILE")
	}
	req := host.Request{
		Command:    a.commandLine(),
		IDE:        os.Getenv("AXX_IDE"),
		Dir:        cfg.Dir,
		Profile:    profile,
		Properties: props,
		Settings:   cf.settings,
		Services:   services,
	}
	if cfg.File != "" {
		if rel, err := filepath.Rel(cfg.Dir, cfg.File); err == nil {
			req.Config = filepath.ToSlash(rel)
		}
	}
	return req, nil
}

// commandLine is how this axx was started, for the host's person to read.
func (a *App) commandLine() string {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "__supervise" {
		args = append([]string{"up"}, args[1:]...)
	}
	return strings.Join(append([]string{"axx"}, args...), " ")
}

// openHost asks the host for the services: their output goes to out, and
// what the run waits for to status.
func (a *App) openHost(ctx context.Context, ann *host.Announcement, req host.Request, out, status io.Writer) (*host.Lease, error) {
	fmt.Fprintf(status, "axx: asking the axx host (%s) for %s\n", ann.Addr, servicesText(req.Services))
	return host.Open(ctx, ann, req, out, status)
}

// notListening reports whether err is a host's that does not answer: its
// runs start the services themselves, with a warning.
func notListening(err error) (string, bool) {
	var ae *axxerr.Error
	if errors.As(err, &ae) && ae.Code == host.CodeNotListening {
		return ae.Message, true
	}
	return "", false
}

func servicesText(names []string) string {
	if names == nil {
		return "the services"
	}
	return strings.Join(names, ", ")
}
