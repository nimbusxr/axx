package host

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// helperEnv makes the test binary act as a service or a tool (see runHelper).
const helperEnv = "AXX_HOST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runHelper implements the fakes. Modes:
//
//	serve <line>              print line, run until stopped
//	append <file> <text>      append a line to file
//	compose ...               as `docker compose ... config`: print the model
//	                          AXX_HOST_MODEL names, and some of its environment
func runHelper(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "serve":
		fmt.Println(strings.Join(args[1:], " "))
		time.Sleep(time.Hour)
		return 0
	case "append":
		f, err := os.OpenFile(args[1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return 1
		}
		defer f.Close()
		_, _ = fmt.Fprintln(f, args[2])
		return 0
	case "compose":
		b, err := os.ReadFile(os.Getenv("AXX_HOST_MODEL"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		_, _ = os.Stdout.Write(b)
		// What of its environment steers it, and what its files may use.
		fmt.Printf("x-env: DOCKER_CONFIG=%s PARCELS_TAG=%s\n", os.Getenv("DOCKER_CONFIG"), os.Getenv("PARCELS_TAG"))
		return 0
	}
	return 2
}
