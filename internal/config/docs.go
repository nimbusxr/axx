package config

// DefaultDocsPort is the port `axx docs serve` serves the docs on: 7357, TEST
// in digits, which no common developer tool uses.
const DefaultDocsPort = 7357

// Docs is where the project's people read axx's documentation: the port `axx
// docs serve` serves it on, and the copy the links axx prints lead to (an
// organization's own, or `axx docs serve`'s), for a network that cannot reach
// the public site.
type Docs struct {
	// Port is the port `axx docs serve` serves the docs on, on 127.0.0.1: 7357
	// unless set. Its --port flag wins.
	Port int `json:"port,omitempty" jsonschema:"minimum=1,maximum=65535"`
	// URL is the address of the docs site the links axx prints lead to (the
	// docs line of an error, the help's last line): an organization's copy,
	// like https://axx-docs.example.com, or http://127.0.0.1:7357 for `axx docs
	// serve`. The AXX_DOCS_URL environment variable wins; without either, they
	// lead to https://axx.nimbusxr.us.
	URL string `json:"url,omitempty" jsonschema:"format=uri,pattern=^https?://[^/?#]+(/[^?#]*)?$"`
}

// DocsPort is the port `axx docs serve` serves the docs on, as axx.yaml says.
func (c *Config) DocsPort() int {
	if c == nil || c.Docs.Port == 0 {
		return DefaultDocsPort
	}
	return c.Docs.Port
}
