// Package shellwords splits command lines without a shell, exactly like the
// original implementation: double-quoted segments become one word (quotes
// removed); everything else splits on whitespace. There is no escaping,
// globbing or variable expansion; use `shell: true` for shell features.
package shellwords

import "regexp"

var word = regexp.MustCompile(`"([^"]*)"|(\S+)`)

// Split tokenizes a command line.
func Split(line string) []string {
	var out []string
	for _, m := range word.FindAllStringSubmatch(line, -1) {
		if m[2] != "" {
			out = append(out, m[2])
		} else {
			out = append(out, m[1])
		}
	}
	return out
}
