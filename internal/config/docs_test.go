package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
)

func TestDocs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", "docs:\n  port: 9123\n  url: https://docs.example.com/axx\n")
	cfg, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DocsPort() != 9123 || cfg.Docs.URL != "https://docs.example.com/axx" {
		t.Fatalf("docs = %+v", cfg.Docs)
	}

	write(t, dir, "axx.yaml", "version: 1\n")
	if cfg, err = Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)}); err != nil {
		t.Fatal(err)
	}
	if cfg.DocsPort() != DefaultDocsPort || cfg.Docs.URL != "" {
		t.Fatalf("without docs: port %d, url %q", cfg.DocsPort(), cfg.Docs.URL)
	}
	if (*Config)(nil).DocsPort() != DefaultDocsPort {
		t.Fatal("no project: the default port")
	}
}

func TestDocsRejected(t *testing.T) {
	for name, doc := range map[string]string{
		"port 0":              "docs:\n  port: 0\n",
		"port past 65535":     "docs:\n  port: 70000\n",
		"a port of words":     "docs:\n  port: seven\n",
		"a url of no http":    "docs:\n  url: ftp://docs.example.com/axx\n",
		"a url of no host":    "docs:\n  url: https://\n",
		"a url with a query":  "docs:\n  url: https://docs.example.com/axx?v=1\n",
		"a relative url":      "docs:\n  url: /axx\n",
		"an unknown docs key": "docs:\n  host: 0.0.0.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "axx.yaml", doc)
			_, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
			var ae *axxerr.Error
			if !errors.As(err, &ae) || ae.Code != CodeInvalid || !strings.Contains(err.Error(), "docs") {
				t.Fatalf("want an invalid configuration naming docs, got %v", err)
			}
		})
	}
}
