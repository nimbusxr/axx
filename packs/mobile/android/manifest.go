package mobileandroid

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

// apkPackage is the package an APK installs: the package attribute of its
// manifest, which an APK keeps in Android's binary XML.
func apkPackage(apk string) (string, error) {
	z, err := zip.OpenReader(apk)
	if err != nil {
		return "", err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != "AndroidManifest.xml" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, 16<<20))
		if err != nil {
			return "", err
		}
		pkg, err := manifestPackage(b)
		if err != nil {
			return "", fmt.Errorf("%s's manifest: %w", apk, err)
		}
		return pkg, nil
	}
	return "", fmt.Errorf("%s has no AndroidManifest.xml: is it an APK?", apk)
}

// The chunks of Android's binary XML that the manifest's package needs.
const (
	chunkXML          = 0x0003
	chunkStringPool   = 0x0001
	chunkStartElement = 0x0102
	utf8Strings       = 1 << 8
	typeString        = 0x03
)

// manifestPackage reads the package attribute of a binary manifest's
// <manifest> element.
func manifestPackage(b []byte) (string, error) {
	le := binary.LittleEndian
	if len(b) < 8 || le.Uint16(b) != chunkXML {
		return "", errors.New("not Android's binary XML")
	}
	var pool []string
	for off := int(le.Uint16(b[2:])); off+8 <= len(b); {
		kind, size := le.Uint16(b[off:]), int(le.Uint32(b[off+4:]))
		if size < 8 || off+size > len(b) {
			return "", errors.New("a chunk runs past the end")
		}
		chunk := b[off : off+size]
		switch kind {
		case chunkStringPool:
			p, err := stringPool(chunk)
			if err != nil {
				return "", err
			}
			pool = p
		case chunkStartElement:
			if len(chunk) < 36 {
				return "", errors.New("an element too short")
			}
			name := int(le.Uint32(chunk[20:]))
			if name >= len(pool) || pool[name] != "manifest" {
				break
			}
			start, each, count := int(le.Uint16(chunk[24:])), int(le.Uint16(chunk[26:])), int(le.Uint16(chunk[28:]))
			for i := range count {
				a := 16 + start + i*each
				if a+20 > len(chunk) {
					break
				}
				attr := int(le.Uint32(chunk[a+4:]))
				if attr >= len(pool) || pool[attr] != "package" {
					continue
				}
				raw := le.Uint32(chunk[a+8:])
				if raw == 0xffffffff && chunk[a+15] == typeString {
					raw = le.Uint32(chunk[a+16:])
				}
				if int(raw) < len(pool) {
					return pool[raw], nil
				}
			}
			return "", errors.New("the <manifest> element names no package")
		}
		off += size
	}
	return "", errors.New("no <manifest> element")
}

// stringPool reads a string pool chunk's strings: UTF-8 or UTF-16.
func stringPool(c []byte) ([]string, error) {
	le := binary.LittleEndian
	if len(c) < 28 {
		return nil, errors.New("a string pool too short")
	}
	header, count, flags, start := int(le.Uint16(c[2:])), int(le.Uint32(c[8:])), le.Uint32(c[16:]), int(le.Uint32(c[20:]))
	if header+4*count > len(c) || start > len(c) {
		return nil, errors.New("a string pool runs past its chunk")
	}
	out := make([]string, count)
	for i := range count {
		at := start + int(le.Uint32(c[header+4*i:]))
		if at >= len(c) {
			return nil, errors.New("a string runs past its pool")
		}
		s, err := poolString(c[at:], flags&utf8Strings != 0)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func poolString(b []byte, utf8 bool) (string, error) {
	le := binary.LittleEndian
	if utf8 {
		// The length in UTF-16 units, then in bytes: one byte each, or two
		// when the first has its high bit.
		n := 0
		for range 2 {
			if len(b) == 0 {
				return "", errors.New("a string's length is cut off")
			}
			n = int(b[0])
			if n&0x80 != 0 {
				if len(b) < 2 {
					return "", errors.New("a string's length is cut off")
				}
				n = (n&0x7f)<<8 | int(b[1])
				b = b[2:]
			} else {
				b = b[1:]
			}
		}
		if n > len(b) {
			return "", errors.New("a string runs past its pool")
		}
		return string(b[:n]), nil
	}
	if len(b) < 2 {
		return "", errors.New("a string's length is cut off")
	}
	n := int(le.Uint16(b))
	b = b[2:]
	if n&0x8000 != 0 {
		if len(b) < 2 {
			return "", errors.New("a string's length is cut off")
		}
		n = (n&0x7fff)<<16 | int(le.Uint16(b))
		b = b[2:]
	}
	if 2*n > len(b) {
		return "", errors.New("a string runs past its pool")
	}
	u := make([]uint16, n)
	for i := range u {
		u[i] = le.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u)), nil
}
