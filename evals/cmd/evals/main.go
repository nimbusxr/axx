// Command evals runs axx's agent evaluations on Harbor.
//
//	go run ./cmd/evals images                  build the Docker images the tasks use
//	go run ./cmd/evals sync [--check]          regenerate each task's generated files
//	go run ./cmd/evals check [--solution|--nop] TASK...
//	                                           verify a task locally, without Harbor
//	go run ./cmd/evals prepare --condition C --out DIR
//	                                           a Harbor dataset for one condition
//	go run ./cmd/evals run --agent A [--model M] [--conditions none,skills,mcp,both]
//	                                           images + prepare + harbor run + report
//	go run ./cmd/evals report --agent A --job COND=DIR...
//	                                           aggregate Harbor jobs into a result file
//	go run ./cmd/evals compare BASELINE RESULTS [--max-drop 10]
//	                                           exit 1 when a condition's score dropped
//
// Run it from the evals directory (or pass --root).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var commands = map[string]func(args []string) error{
	"images":  cmdImages,
	"sync":    cmdSync,
	"check":   cmdCheck,
	"prepare": cmdPrepare,
	"run":     cmdRun,
	"report":  cmdReport,
	"compare": cmdCompare,
	"packs":   cmdPacks,
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		usage()
		return
	}
	f, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "evals: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err := f(os.Args[2:]); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, "evals:", ee.msg)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "evals:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`evals runs axx's agent evaluations on Harbor (https://harborframework.com).

Commands:
  images    build the Docker images the tasks use (axx from this checkout)
  sync      regenerate each task's generated files (--check: fail if stale)
  check     verify tasks locally without Harbor (--solution, --nop)
  prepare   write a Harbor dataset for one condition
  run       build, prepare and run every condition for an agent, then report
  report    aggregate Harbor jobs into evals/results/<date>-<agent>.json and .md
  compare   compare a result file with a baseline; exit 1 on a regression
  packs     list the axx packs in the evals image

Run "go run ./cmd/evals <command> -h" for a command's flags. See README.md.
`)
}

// exitError ends the program with a code.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

// evalsRoot finds the evals directory: the given one, or the nearest parent
// of the working directory that holds tasks/ and cmd/evals.
func evalsRoot(given string) (string, error) {
	if given != "" {
		return filepath.Abs(given)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for d := wd; ; d = filepath.Dir(d) {
		for _, c := range []string{d, filepath.Join(d, "evals")} {
			if isDir(filepath.Join(c, "tasks")) && isDir(filepath.Join(c, "cmd", "evals")) {
				return c, nil
			}
		}
		if filepath.Dir(d) == d {
			return "", errors.New("cannot find the evals directory; run from evals/ or pass --root")
		}
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
