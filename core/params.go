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
// {ordinal}, {pattern}, {mimeType}, {duration} and {filepath}. It is always
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
				Regexps: []string{`([^\s]+)`},
				Doc: "a file of the project, with no spaces: a path relative to the `resources` directories or to " +
					"axx.yaml's directory, or an absolute path",
				Examples: []string{"seeds/parcels.yaml", "kafka/scan-delivered.json"},
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
