package md

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// ErrorCodesPage renders the error-code and exit-code reference. Heading
// anchors are the lower-cased codes, which is what axxerr.DocsURL links to.
func ErrorCodesPage(frontmatter bool) (string, error) {
	entries := axxerr.Catalog()
	if len(entries) == 0 {
		return "", ErrEmpty
	}
	var b strings.Builder
	if frontmatter {
		b.WriteString("---\ntitle: Exit and error codes\ndescription: The process exit codes, and every AXX-Exxxx error code with what it means and how to fix it.\n---\n\n")
	}
	b.WriteString(GeneratedHeader)
	if !frontmatter {
		b.WriteString("\n# Exit and error codes\n")
	}
	b.WriteString("\nEvery user-facing error has a stable code. `axx explain AXX-E0102` prints an entry from this page; " +
		"with `--json`, errors appear in `errors[]` with `code`, `message`, `location`, `hint` and `docs`.\n")
	b.WriteString("\n## Exit codes\n\n| Code | Meaning |\n|---|---|\n")
	for _, c := range []exitcode.Code{exitcode.OK, exitcode.Failed, exitcode.Usage, exitcode.Undefined, exitcode.Environment, exitcode.Plugin, exitcode.Interrupted} {
		fmt.Fprintf(&b, "| %d | %s |\n", int(c), exitMeaning[c])
	}
	for _, r := range axxerr.Ranges {
		var in []axxerr.Entry
		for _, e := range entries {
			if e.Code >= r.From && e.Code <= r.To {
				in = append(in, e)
			}
		}
		if len(in) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", r.Area)
		for _, e := range in {
			fmt.Fprintf(&b, "\n### %s\n\n**%s** · exit %d\n\n%s\n\n**Fix:** %s\n", e.Code, e.Title, int(e.Exit), e.Meaning, e.Fix)
		}
	}
	return b.String(), nil
}

var exitMeaning = map[exitcode.Code]string{
	exitcode.OK:          "Success: every selected scenario passed (or the command succeeded).",
	exitcode.Failed:      "At least one scenario failed.",
	exitcode.Usage:       "Usage or configuration error: bad flags, invalid `axx.yaml`, unparsable features.",
	exitcode.Undefined:   "Undefined or ambiguous steps, or lint violations.",
	exitcode.Environment: "Environment or lifecycle failure: an app did not start, become ready or stop.",
	exitcode.Plugin:      "Reserved (not used).",
	exitcode.Interrupted: "Interrupted (Ctrl-C); apps were stopped and cleaned up.",
}
