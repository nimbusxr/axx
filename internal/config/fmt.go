package config

// Fmt is how `axx fmt`, and editors through axx lsp, lay out the project's
// feature files. Without it, they are laid out as axx lays them out: two
// spaces to a level.
type Fmt struct {
	// Indent is the spaces of one level of indentation: 2 (the default) or 4.
	Indent int `json:"indent,omitempty" jsonschema:"enum=2,enum=4"`
}
