package controlsnist

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// slimSHA256 is the hash of what the pack embeds of NIST's catalog: what
// internal/tools/controlsdata wrote from the file the pack pins.
const slimSHA256 = "a5906b7f8707f83e40a53a07bf0372991933d9401ce4e3738e434ded713cab75"

func TestTheControlsAreOfThePinnedCatalog(t *testing.T) {
	raw, err := Raw()
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != slimSHA256 {
		t.Fatalf("the embedded controls' SHA-256 is %s, want %s: run internal/tools/controlsdata", hex.EncodeToString(sum[:]), slimSHA256)
	}
}

// The catalog's controls and enhancements, each with its title and its family,
// an enhancement with the control it is of. The pack carries no list.
func TestTheCatalogsControls(t *testing.T) {
	f := Framework()
	if f.Key != "nist" || f.Version != "5.2.0" || len(f.Controls) != 1196 || len(f.Lists) != 0 || f.Notice != "" {
		t.Fatalf("the framework: %s %s, %d controls, %d lists", f.Key, f.Version, len(f.Controls), len(f.Lists))
	}
	byID := map[string]string{}
	for _, c := range f.Controls {
		if c.Title == "" || c.Group == "" || byID[c.ID] != "" {
			t.Errorf("the control %q: %+v", c.ID, c)
		}
		byID[c.ID] = c.Title + " | " + c.Group
	}
	for id, want := range map[string]string{
		"IA-5":   "Authenticator Management | Identification and Authentication",
		"IA-5.1": "Password-based Authentication | Identification and Authentication, IA-5 Authenticator Management",
		"AC-7":   "Unsuccessful Logon Attempts | Access Control",
	} {
		if byID[id] != want {
			t.Errorf("%s is %q, want %q", id, byID[id], want)
		}
	}
	if !strings.HasSuffix(byID["AC-13"], "(withdrawn) | Access Control") {
		t.Errorf("a withdrawn control: %q", byID["AC-13"])
	}
}

// An enhancement is written in brackets in SP 800-53, with a dot in OSCAL and
// in a tag, and with leading zeros in some lists: they name one control.
func TestTheWaysAControlsIDIsWritten(t *testing.T) {
	for _, same := range [][2]string{{"IA-5.1", "IA-5(1)"}, {"ia-5.1", "IA-05(01)"}, {"AC-7", "ac-07"}, {"AC-2.13", "AC-2(13)"}} {
		if !Same(same[0], same[1]) {
			t.Errorf("%s and %s are the same control", same[0], same[1])
		}
	}
	for _, other := range [][2]string{{"IA-5", "IA-5.1"}, {"AC-2.1", "AC-2.13"}, {"AC-7", "AU-7"}, {"IA-5", "password"}} {
		if Same(other[0], other[1]) {
			t.Errorf("%s and %s are two controls", other[0], other[1])
		}
	}
}

func TestThePack(t *testing.T) {
	m := Pack().Manifest()
	if m.Name != "controls-nist" || !m.Experimental || len(m.Requires) != 1 || m.Requires[0] != "controls-core" || len(m.Steps) != 0 {
		t.Errorf("the manifest: %+v", m)
	}
}
