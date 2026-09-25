package config

// Fixtures configures `axx fixtures`, the fixture factory: *.factory.yaml,
// *.fixture.yaml and *.prototype.yaml sources expanded into schema-governed
// fixture files, with the ownership manifest, drift and conformance checks.
type Fixtures struct {
	// BaseDir is the resource root every fixtures path resolves against,
	// relative to axx.yaml. Default: the directory of axx.yaml.
	BaseDir string `json:"baseDir,omitempty"`
	// Factories are globs (relative to baseDir) locating factory specs.
	// Default: ["**.factory.yaml"].
	Factories []string `json:"factories,omitempty"`
	// Sources are additional roots, relative to baseDir ("../" reaches
	// outside it), walked for factory, prototype and fixture sources exactly
	// like baseDir, so fixtures can sit next to generated files that live
	// elsewhere (a compose-mounted WireMock directory, say). Roots must
	// exist and be disjoint from baseDir and each other.
	Sources []string `json:"sources,omitempty"`
	// Conformance rules validate files no factory manages through a
	// family's schema oracle.
	Conformance []FixtureConformance `json:"conformance,omitempty"`
	// Lint configures the lint rules generated from identity declarations.
	Lint FixtureLint `json:"lint,omitzero"`
	// Output holds module-wide output defaults; factories override them
	// with factory.output.
	Output FixtureOutput `json:"output,omitzero"`
}

// FixtureConformance validates matched files against a schema.
type FixtureConformance struct {
	// Name is shown in check output.
	Name string `json:"name"`
	// FilePatterns are globs (relative to baseDir) of the files to validate.
	FilePatterns []string `json:"filePatterns" jsonschema:"minItems=1"`
	// SchemaType is the family whose oracle validates the files: avro,
	// json, yaml, xml, protobuf or dataset.
	SchemaType string `json:"schemaType" jsonschema:"example=avro,example=json,example=yaml,example=xml,example=protobuf,example=dataset"`
	// SchemaRef is the schema, relative to baseDir (unlike factory.schema,
	// which is relative to the factory file).
	SchemaRef string `json:"schemaRef"`
}

// FixtureLint configures the generated lint rules.
type FixtureLint struct {
	// Emit writes one cross-file-unique lint rule per declared identity.
	// Default: true.
	Emit *bool `json:"emit,omitempty"`
	// Output is the generated rules file, relative to baseDir. Default:
	// axx-lint.generated.yaml. Wire it in with lint.include.
	Output string `json:"output,omitempty"`
}

// FixtureOutput holds output defaults.
type FixtureOutput struct {
	// Ignored makes outputs gitignored derivations instead of committed
	// files: the tool maintains exact .gitignore entries and `axx fixtures
	// generate` materializes them. Default: false.
	Ignored bool `json:"ignored,omitempty"`
}
