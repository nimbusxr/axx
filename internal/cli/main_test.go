package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/version"
	"github.com/nimbusxr/axx/packs/all"
)

// helperEnv makes the test binary a service: it prints its arguments ("parcels
// api ready"), then, until stopped, the first but last ("parcels api serving")
// every 100ms; with "append <file> <text>" it appends text to file and exits.
// "tell <name=value>" before them tells axx a property first, in the file
// $AXX_PROPERTIES names; alone, it tells it and exits, as a hook. "cover
// <file> <text>" before them writes text (\n a new line) to the file in its
// coverage folder ($AXX_COVERAGE_DIR), as a service's own coverage tool.
const helperEnv = "AXX_CLI_HELPER"

// installedEnv makes the test binary the axx installed on a machine, a release of the
// version it names with no packs in it. installedAPIEnv is where it finds its releases.
const (
	installedEnv    = "AXX_CLI_INSTALLED"
	installedAPIEnv = "AXX_CLI_INSTALLED_API"
)

// nestedEnv is set for what a test starts from the test binary. Without a role above, that
// would run every test again, each starting more: it stops there instead.
const nestedEnv = "AXX_CLI_NESTED"

// TestMain builds every pack into the test binary, as axx builds itself
// with a project's packs.
func TestMain(m *testing.M) {
	if v := os.Getenv(installedEnv); v != "" {
		version.Version = v
		if api := os.Getenv(installedAPIEnv); api != "" {
			installed := newUpgradeEnv
			newUpgradeEnv = func() (*upgradeEnv, error) {
				env, err := installed()
				if env != nil {
					env.client.API, env.attest = api, nil
				}
				return env, err
			}
		}
		os.Exit(Main(os.Args[1:]))
	}
	if os.Getenv(helperEnv) == "1" {
		args := os.Args[1:]
		if len(args) == 4 && args[0] == "hook" {
			os.Exit(hookHelper(args[1], args[2], args[3]))
		}
		if len(args) >= 2 && args[0] == "tell" {
			f, err := os.OpenFile(os.Getenv("AXX_PROPERTIES"), os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				os.Exit(1)
			}
			_, _ = fmt.Fprintln(f, args[1])
			_ = f.Close()
			if args = args[2:]; len(args) == 0 {
				os.Exit(0)
			}
		}
		if len(args) >= 3 && args[0] == "cover" {
			text := strings.ReplaceAll(args[2], `\n`, "\n")
			if err := os.WriteFile(filepath.Join(os.Getenv("AXX_COVERAGE_DIR"), args[1]), []byte(text), 0o644); err != nil {
				os.Exit(1)
			}
			args = args[3:]
		}
		if len(args) == 3 && args[0] == "append" {
			f, err := os.OpenFile(args[1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				os.Exit(1)
			}
			_, _ = fmt.Fprintln(f, args[2])
			_ = f.Close()
			os.Exit(0)
		}
		fmt.Println(strings.Join(args, " "))
		for {
			time.Sleep(100 * time.Millisecond)
			fmt.Println(strings.Join(args[:len(args)-1], " ") + " serving")
		}
	}
	if os.Getenv(nestedEnv) != "" {
		fmt.Fprintln(os.Stderr, "the test binary started with no role, as if it were axx")
		os.Exit(125)
	}
	for k, p := range all.Packs() {
		engine.Register(k, p)
	}
	os.Exit(m.Run())
}
