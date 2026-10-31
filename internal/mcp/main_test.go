package mcp

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// helperEnv makes the test binary a service: it prints its arguments and
// runs until stopped; with "append <file> <text>" it appends text to file
// and exits.
const helperEnv = "AXX_MCP_HELPER"

// TestMain builds every pack into the test binary, as axx builds itself
// with a project's packs.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		args := os.Args[1:]
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
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	for k, p := range all.Packs() {
		engine.Register(k, p)
	}
	os.Exit(m.Run())
}
