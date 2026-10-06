package core

import (
	"fmt"
	"strconv"
	"time"
)

// Supported MIME types for request/response payload steps.
const (
	MimeJSON        = "application/json"
	MimeTextJSON    = "text/json"
	MimeProblemJSON = "application/problem+json"
	MimeForm        = "application/x-www-form-urlencoded"
)

// SupportedMimeTypes lists the values {mimeType} accepts, in docs order.
var SupportedMimeTypes = []string{MimeJSON, MimeTextJSON, MimeProblemJSON, MimeForm}

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
				Doc:      "a content type",
				Values:   SupportedMimeTypes,
				Examples: []string{"application/json"},
				Transform: func(_ *Scenario, match string, _ []*string) (any, error) {
					for _, m := range SupportedMimeTypes {
						if m == match {
							return m, nil
						}
					}
					return nil, fmt.Errorf("Unsupported content type: %s", match) //nolint:staticcheck // user-facing message
				},
			},
			{
				Name:    "filepath",
				Regexps: []string{`([^\s]+)`, `"([^"]+)"`},
				Doc: "a file of the project: a path relative to the `resources` directories or to " +
					"axx.yaml's directory, or an absolute path, quoted when it has a space",
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
				Regexps:  []string{`(\d+)(s|m)`},
				Doc:      "a duration in seconds (`s`) or minutes (`m`)",
				Examples: []string{"5s", "2m"},
				Transform: func(_ *Scenario, match string, groups []*string) (any, error) {
					if len(groups) < 2 || groups[0] == nil || groups[1] == nil {
						return nil, fmt.Errorf("invalid duration %q", match)
					}
					n, err := strconv.ParseInt(*groups[0], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid duration %q", match)
					}
					unit := time.Second
					if *groups[1] == "m" {
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
