// Command controlsdata fetches the frameworks the controls packs carry from their publishers,
// each at a pinned commit and checked against a pinned SHA-256, and writes what the packs embed:
// OWASP's ASVS as OWASP publishes it, and NIST's SP 800-53 cut down to its controls' IDs, titles
// and families. It fetches NIST's schemas of OSCAL's assessment results too, which the
// controls-oscal pack's tests hold its documents to (controlsoscal.Schemas). It is run by hand, from the module's root, when a pack moves to another version
// of its framework; go generate downloads nothing.
//
//	go run ./internal/tools/controlsdata
//
// The packs' tests hold what is embedded to the hashes here (controlsasvs.SourceSHA256,
// controlsnist.SourceSHA256).
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nimbusxr/axx/internal/mirror"
	controlsasvs "github.com/nimbusxr/axx/packs/controls/asvs"
	controlsnist "github.com/nimbusxr/axx/packs/controls/nist"
	controlsoscal "github.com/nimbusxr/axx/packs/controls/oscal"
)

func main() {
	asvs, err := fetch(controlsasvs.Source, controlsasvs.SourceSHA256)
	if err != nil {
		fail(err)
	}
	// As OWASP publishes it, compressed: the pack reads the file itself.
	if err := writeGzip(filepath.Join("packs", "controls", "asvs", "data", "asvs.flat.json.gz"), asvs); err != nil {
		fail(err)
	}
	nist, err := fetch(controlsnist.Source, controlsnist.SourceSHA256)
	if err != nil {
		fail(err)
	}
	slim, err := controlsnist.Slim(nist)
	if err != nil {
		fail(err)
	}
	if err := writeGzip(filepath.Join("packs", "controls", "nist", "data", "sp800-53.json.gz"), slim); err != nil {
		fail(err)
	}
	for _, s := range controlsoscal.Schemas {
		schema, err := fetch(s.Source, s.SHA256)
		if err != nil {
			fail(err)
		}
		if err := writeGzip(filepath.Join("packs", "controls", "oscal", "testdata", s.File), schema); err != nil {
			fail(err)
		}
	}
	fmt.Println("wrote the controls packs' data: run their tests, and say in THIRD_PARTY_NOTICES.md what changed")
}

// fetch downloads a file of a GitHub repository at a commit (path is what follows github.com),
// through the mirror AXX_GITHUB_URL names when one is set, and checks it against its hash.
func fetch(path, sum string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirror.GitHub(path), nil)
	if err != nil {
		return nil, mirror.RedactErr(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, mirror.RedactErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s (%s)", path, resp.Status, mirror.GitHubHelp())
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	if got := hex.EncodeToString(h[:]); got != sum {
		return nil, fmt.Errorf("%s: its SHA-256 is %s, and the pack pins %s", path, got, sum)
	}
	return b, nil
}

// writeGzip writes data compressed, the same bytes whenever it is run: no name and no time in
// the file.
func writeGzip(path string, data []byte) error {
	var b bytes.Buffer
	w, err := gzip.NewWriterLevel(&b, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if !json.Valid(data) {
		return fmt.Errorf("%s: what was fetched is not JSON", path)
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "controlsdata:", err)
	os.Exit(1)
}
