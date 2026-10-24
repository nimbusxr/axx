// Package foundationmodels writes stubs of Foundation Models language models:
// a Swift type of the LanguageModel protocol (iOS, macOS and visionOS 27),
// which an app's test build gives its sessions in place of
// SystemLanguageModel, and which answers a prompt as its mappings say.
//
// Apple's model comes with the system, and the system has no setting that
// puts another in its place: an app that is tested with a stub selects it in
// its test build, as it selects the system's model in the others.
package foundationmodels

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "foundation-models", Name: "Foundation Models", Extensions: []string{".swift"}, API: true, Write: format.File(write)})
}

//go:embed model.swift.tmpl
var source string

var model = template.Must(template.New("model").Parse(source))

// details are a Foundation Models model's own settings in its contract.
type details struct {
	// Name is the stub's Swift type, which the app's test build gives its
	// sessions: DeliveryNoteModel.
	Name string `json:"name"`
	// Capabilities are what the model can do, as LanguageModelCapabilities
	// names them: the system model's, without them.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Capabilities are the capabilities of LanguageModelCapabilities.
var Capabilities = []string{"guidedGeneration", "reasoning", "toolCalling", "vision"}

// systemCapabilities are the system model's: a stub's, unless its contract
// says otherwise.
var systemCapabilities = []string{"guidedGeneration", "toolCalling", "vision"}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// keywords are Swift's keywords that are not a type's name.
var keywords = []string{
	"Any", "Self", "Type", "Protocol", "as", "associatedtype", "break", "case", "catch", "class", "continue",
	"default", "defer", "deinit", "do", "else", "enum", "extension", "fallthrough", "false", "fileprivate",
	"for", "func", "guard", "if", "import", "in", "init", "inout", "internal", "is", "let", "nil", "open",
	"operator", "private", "protocol", "public", "repeat", "rethrows", "return", "self", "static",
	"struct", "subscript", "super", "switch", "throw", "throws", "true", "try", "typealias", "var",
	"where", "while",
}

func write(s *stub.Stub) ([]byte, error) {
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a Foundation Models stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...} or {"json": ...}`)
	}
	d, err := detailsOf(s.Contract)
	if err != nil {
		return nil, err
	}
	type mapping struct{ Name, Text, Answer string }
	data := struct {
		Name, Capabilities, Default, DefaultName string
		Mappings                                 []mapping
	}{Name: d.Name, Default: swiftString(s.Generated[len(s.Texts)]), DefaultName: oneLine(s.Names[len(s.Texts)])}
	capabilities := make([]string, len(d.Capabilities))
	for i, c := range d.Capabilities {
		capabilities[i] = "." + c
	}
	data.Capabilities = strings.Join(capabilities, ", ")
	for i, text := range s.Texts {
		data.Mappings = append(data.Mappings, mapping{oneLine(s.Names[i]), swiftString(text), swiftString(s.Generated[i])})
	}
	var buf bytes.Buffer
	if err := model.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// detailsOf reads the contract's details: its type's name, and its
// capabilities.
func detailsOf(c *contract.Contract) (details, error) {
	var d details
	if len(c.Format.Details) > 0 {
		dec := json.NewDecoder(bytes.NewReader(c.Format.Details))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			return d, contractError("its format.details are not a Foundation Models model's: %v", err)
		}
	}
	switch {
	case d.Name == "":
		return d, contractError("format.details.name is missing: the stub's Swift type, which the app's test build gives its sessions")
	case !identifier.MatchString(d.Name) || slices.Contains(keywords, d.Name):
		return d, contractError("format.details.name %q is not a Swift type's name", d.Name)
	}
	if d.Capabilities == nil {
		d.Capabilities = systemCapabilities
	}
	for _, c := range d.Capabilities {
		if !slices.Contains(Capabilities, c) {
			return d, contractError("format.details.capabilities has %q, which is not one of %s", c, strings.Join(Capabilities, ", "))
		}
	}
	return d, nil
}

func contractError(f string, args ...any) error {
	return axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: "+f, args...).
		WithHint(`a Foundation Models model's contract is {"format": {"type": "foundation-models", "details": {"name": "DeliveryNoteModel"}}, "task": "text-generation"}: the stubs guide shows it`)
}

// swiftString is a Swift string literal of s.
func swiftString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20, r == 0x7f, r == 0x2028, r == 0x2029:
			fmt.Fprintf(&b, `\u{%x}`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// oneLine is a mapping's name as a comment has it: on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
