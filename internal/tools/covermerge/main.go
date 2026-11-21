// SPDX-License-Identifier: Apache-2.0

// Command covermerge adds up the coverage of the nightly run's jobs: each job's tests write
// profiles of the statements they ran (go test -coverprofile), over every package of axx, and
// no one job runs them all: the unit tests run on three systems, and each pack's integration
// tests where what it talks to is. A statement counts as covered when any job ran it.
//
//	covermerge [-min percent] [-out merged.out] profiles-or-folders...
//
// It prints the statements covered of all those any profile names, with the parts of axx that
// lack the most, and with -min ends with status 1 when the whole is under it.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const module = "github.com/nimbusxr/axx/"

func main() {
	minimum := flag.Float64("min", 0, "the percentage of statements the profiles must cover together")
	out := flag.String("out", "", "write the merged profile here")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "covermerge: name the profiles, or the folders they are in")
		os.Exit(2)
	}
	blocks := map[string]block{}
	read := 0
	for _, arg := range flag.Args() {
		err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".out" {
				return err
			}
			f, err := os.Open(path) //nolint:gosec // the profiles the command is given
			if err != nil {
				return err
			}
			defer f.Close()
			read++
			return merge(blocks, f)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "covermerge:", err)
			os.Exit(2)
		}
	}
	if read == 0 {
		fmt.Fprintln(os.Stderr, "covermerge: no profile (*.out) in", strings.Join(flag.Args(), ", "))
		os.Exit(2)
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(profile(blocks)), 0o644); err != nil { //nolint:gosec // a report, for anyone to read
			fmt.Fprintln(os.Stderr, "covermerge:", err)
			os.Exit(2)
		}
	}
	total := report(os.Stdout, blocks, read)
	if total < *minimum {
		fmt.Printf("\nUnder the minimum of %.1f%%.\n", *minimum)
		os.Exit(1)
	}
}

// block is a run of statements a profile names, and whether any job ran it.
type block struct {
	statements int
	ran        bool
}

// merge adds a profile's blocks to those read so far.
func merge(blocks map[string]block, r io.Reader) error {
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for lines.Scan() {
		line := lines.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// file:start,end statements count
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return fmt.Errorf("not a line of a coverage profile: %q", line)
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil {
			return fmt.Errorf("not a line of a coverage profile: %q", line)
		}
		count, err := strconv.ParseUint(fields[2], 10, 64)
		if err != nil {
			return fmt.Errorf("not a line of a coverage profile: %q", line)
		}
		b := blocks[fields[0]]
		b.statements = statements
		b.ran = b.ran || count > 0
		blocks[fields[0]] = b
	}
	return lines.Err()
}

// profile is the merged blocks as a profile go tool cover reads.
func profile(blocks map[string]block) string {
	keys := make([]string, 0, len(blocks))
	for k := range blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("mode: set\n")
	for _, k := range keys {
		ran := 0
		if blocks[k].ran {
			ran = 1
		}
		fmt.Fprintf(&b, "%s %d %d\n", k, blocks[k].statements, ran)
	}
	return b.String()
}

// part is the part of axx a file is in: a pack, or a folder of internal, or a top folder.
func part(file string) string {
	dirs := strings.Split(strings.TrimPrefix(file, module), "/")
	dirs = dirs[:len(dirs)-1]
	depth := 1
	if len(dirs) > 1 && (dirs[0] == "packs" || dirs[0] == "internal") {
		depth = 2
		// The families of packs: a pack is one folder further in.
		switch dirs[1] {
		case "web", "aws", "gcp", "azure", "models", "mobile", "desktop", "coverage":
			if dirs[0] == "packs" && len(dirs) > 2 {
				depth = 3
			}
		}
	}
	if depth > len(dirs) {
		depth = len(dirs)
	}
	return strings.Join(dirs[:depth], "/")
}

// report prints the whole and the parts that lack the most, and returns the whole's percentage.
func report(w io.Writer, blocks map[string]block, profiles int) float64 {
	type sum struct{ all, ran int }
	parts := map[string]*sum{}
	whole := sum{}
	for key, b := range blocks {
		file := key[:strings.LastIndex(key, ":")]
		p := parts[part(file)]
		if p == nil {
			p = &sum{}
			parts[part(file)] = p
		}
		p.all += b.statements
		whole.all += b.statements
		if b.ran {
			p.ran += b.statements
			whole.ran += b.statements
		}
	}
	percent := func(s sum) float64 {
		if s.all == 0 {
			return 0
		}
		return 100 * float64(s.ran) / float64(s.all)
	}
	fmt.Fprintf(w, "%.1f%% of statements: %d of %d, from %d profiles.\n\n", percent(whole), whole.ran, whole.all, profiles)
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := parts[names[i]], parts[names[j]]
		if a.all-a.ran != b.all-b.ran {
			return a.all-a.ran > b.all-b.ran
		}
		return names[i] < names[j]
	})
	fmt.Fprintln(w, "| Part | Statements | Covered | Not run |")
	fmt.Fprintln(w, "| --- | ---: | ---: | ---: |")
	for i, name := range names {
		if i == 25 {
			break
		}
		p := parts[name]
		fmt.Fprintf(w, "| %s | %d | %.1f%% | %d |\n", name, p.all, percent(*p), p.all-p.ran)
	}
	return percent(whole)
}
