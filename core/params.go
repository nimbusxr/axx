package core

import (
	"fmt"
	"mime"
	"strconv"
	"strings"
	"time"
)

// Media types the payload steps treat in a way of their own: JSON's, whose
// payloads the payload property steps edit, and a form's, sent form-encoded.
const (
	MimeJSON        = "application/json"
	MimeTextJSON    = "text/json"
	MimeProblemJSON = "application/problem+json"
	MimeForm        = "application/x-www-form-urlencoded"
)

// MediaType checks that s is a media type a payload can have, like
// application/json, text/plain or text/plain;charset=UTF-8, and returns it as
// written: a type and a subtype, with no wildcard.
func MediaType(s string) (string, error) {
	mt, _, err := mime.ParseMediaType(s)
	kind, sub, slash := strings.Cut(mt, "/")
	if err != nil || !slash || kind == "" || sub == "" || strings.Contains(mt, "*") {
		return "", fmt.Errorf("Not a media type: %s (a media type is a type and a subtype, like application/json or text/plain)", s) //nolint:staticcheck // user-facing message
	}
	return s, nil
}

// ParamsPack returns the "core" pack: the parameter types every pack builds on,
// {ordinal}, {pattern}, {mimeType}, {duration}, {filepath} and {path}. It is always
// loaded.
func ParamsPack() Pack { return paramsPack{} }

type paramsPack struct{}

func (paramsPack) Manifest() Manifest {
	return Manifest{
		Name: "core",
		Doc:  "Parameter types every pack can use.",
		Params: []ParamType{
			{
				Name:     "ordinal",
				Regexps:  []string{`(\d+)(?:st|nd|rd|th)`},
				Doc:      "a position, counting from 1; an optional ordinal left out is the first",
				Examples: []string{"1st", "2nd", "3rd"},
				Transform: func(_ *Scenario, match string, groups []*string) (any, error) {
					if len(groups) == 0 || groups[0] == nil {
						return nil, fmt.Errorf("invalid ordinal %q", match)
					}
					n, err := strconv.ParseInt(*groups[0], 10, 32)
					if err != nil {
						return nil, fmt.Errorf("invalid ordinal %q", match)
					}
					return int(n), nil
				},
			},
			{
				Name:     "pattern",
				Regexps:  []string{`([^\s]+)`},
				Doc:      "a regular expression (Java syntax) with no spaces, which matches the whole value",
				Examples: []string{`PX-\d{4}`, `[A-Z]{2}-\d+`},
			},
			{
				Name:     "mimeType",
				Regexps:  []string{`([^\s]+)`},
				Doc:      "a media type, with no spaces: any that has a type and a subtype",
				Examples: []string{"application/json", "application/x-www-form-urlencoded", "text/plain"},
				Transform: func(_ *Scenario, match string, _ []*string) (any, error) {
					return MediaType(match)
				},
			},
			{
				Name:    "filepath",
				Regexps: []string{`([^\s]+)`, `"([^"]+)"`},
				Doc: "a file of the project: a path relative to the feature file's directory, the `resources` " +
					"directories or axx.yaml's directory (looked for in that order), or an absolute path, quoted when it has a space",
				Examples:  []string{"seeds/parcels.yaml", "kafka/scan-delivered.json", `"seeds/day one.yaml"`},
				Transform: unquoted,
			},
			{
				Name:    "path",
				Regexps: []string{`([^\s"]+)`, `"([^"]+)"`},
				Doc: "a path in a folder or a store, like `manifests/M-1/report.csv`, quoted when it has a space. " +
					"A `*` stands for any characters but a slash, for a name a check cannot know (`snap-*.json`, named by the time): " +
					"the path then names the one file it matches",
				Examples:  []string{"manifests/M-KESTREL-0412/report.csv", `"Parcels/Depot desk/arrivals.json"`, "snap-*.json"},
				Transform: unquoted,
			},
			{
				Name:     "duration",
				Regexps:  []string{`(\d+)(ms|s|m)`},
				Doc:      "a duration in milliseconds (`ms`), seconds (`s`) or minutes (`m`)",
				Examples: []string{"5s", "2m", "500ms"},
				Transform: func(_ *Scenario, match string, groups []*string) (any, error) {
					if len(groups) < 2 || groups[0] == nil || groups[1] == nil {
						return nil, fmt.Errorf("invalid duration %q", match)
					}
					n, err := strconv.ParseInt(*groups[0], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid duration %q", match)
					}
					unit := time.Second
					switch *groups[1] {
					case "ms":
						unit = time.Millisecond
					case "m":
						unit = time.Minute
					}
					return time.Duration(n) * unit, nil
				},
			},
		},
	}
}

// unquoted is a path as written, without the quotes a path with a space
// takes.
func unquoted(_ *Scenario, match string, _ []*string) (any, error) {
	if len(match) >= 2 && match[0] == '"' && match[len(match)-1] == '"' {
		return match[1 : len(match)-1], nil
	}
	return match, nil
}
