//go:build linux

package atspi

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// xauthEntry is an X authority file's entry (Xauth.h).
func xauthEntry(family uint16, addr, number, name string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, family)
	for _, f := range [][]byte{[]byte(addr), []byte(number), []byte(name), data} {
		_ = binary.Write(&b, binary.BigEndian, uint16(len(f)))
		b.Write(f)
	}
	return b.Bytes()
}

func TestCookieFor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Xauthority")
	file := append(append(append(
		xauthEntry(256, "depot", "0", "XDM-AUTHORIZATION-1", []byte{9, 9}),
		xauthEntry(256, "depot", "1", "MIT-MAGIC-COOKIE-1", []byte{0x01, 0x02})...),
		xauthEntry(256, "depot", "2", "MIT-MAGIC-COOKIE-1", []byte{0xab, 0xcd})...),
		xauthEntry(65535, "", "", "MIT-MAGIC-COOKIE-1", []byte{0xff})...)
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
	for number, want := range map[string]string{"2": "abcd", "1": "0102", "7": "ff"} {
		got, err := cookieFor(path, number)
		if err != nil || got != want {
			t.Errorf("display :%s: got %q, %v; want %q", number, got, err, want)
		}
	}
	if err := os.WriteFile(path, xauthEntry(256, "depot", "0", "MIT-MAGIC-COOKIE-1", []byte{1}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cookieFor(path, "3"); err == nil {
		t.Error("a display the file has no cookie for: no error")
	}
}
