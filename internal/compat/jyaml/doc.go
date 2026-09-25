// Package jyaml reads and writes YAML exactly like Jackson's YAML dataformat
// on top of SnakeYAML.
//
// Writing ([Marshal]) reproduces Jackson's YAMLGenerator feature switches
// (MINIMIZE_QUOTES, ALWAYS_QUOTE_NUMBERS_AS_STRINGS, the document start
// marker) and SnakeYAML's Emitter byte for byte: scalar analysis and style
// selection, block indentation (sequences not indented under their key), the
// 80-column line folding of plain and quoted scalars, the explicit "? key"
// form for keys longer than 128 characters, and literal block scalars for
// multi-line strings. Column arithmetic is done in UTF-16 code units, as Java
// does.
//
// Reading ([Unmarshal]) gives scalars the types Jackson's YAMLParser gives
// them when a document is read into a tree: plain scalars are resolved with
// SnakeYAML's YAML 1.1 implicit resolvers (so yes/no/on/off are booleans,
// 0x1F and 017 are integers and timestamps stay strings), quoted and block
// scalars are always strings, and integers become Integer, Long or
// BigInteger by magnitude.
//
// Values use the [jsonx] model: *jsonx.Object for mappings (insertion
// ordered), []any for sequences, string, bool, nil, jsonx.Number, plus []byte
// for !!binary scalars.
//
// The oracle in testdata/oracles/jacksonyaml.json pins all of this to
// Jackson's behavior.
package jyaml
