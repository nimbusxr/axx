package db2

import (
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// The character sets of a database's text, by their CCSIDs, that this client
// reads beside Unicode's.
var charmaps = map[int]*charmap.Charmap{
	819:  charmap.ISO8859_1,
	912:  charmap.ISO8859_2,
	915:  charmap.ISO8859_5,
	813:  charmap.ISO8859_7,
	916:  charmap.ISO8859_8,
	920:  charmap.ISO8859_9,
	923:  charmap.ISO8859_15,
	437:  charmap.CodePage437,
	850:  charmap.CodePage850,
	1250: charmap.Windows1250,
	1251: charmap.Windows1251,
	1252: charmap.Windows1252,
	1253: charmap.Windows1253,
	1254: charmap.Windows1254,
	1255: charmap.Windows1255,
	1256: charmap.Windows1256,
	1257: charmap.Windows1257,
	37:   charmap.CodePage037,
	1047: charmap.CodePage1047,
	1140: charmap.CodePage1140,
}

// readable says whether this client reads text of a character set.
func readable(ccsid int) bool {
	switch ccsid {
	case 0, ccsidUTF8, ccsidUTF16, 13488, 1202, 367, 500:
		return true
	}
	return charmaps[ccsid] != nil
}

// decode reads text of a character set. Text of a set this client does not
// know is read as UTF-8, with a replacement character for what is not.
func decode(b []byte, ccsid int) string {
	switch ccsid {
	case ccsidUTF16, 13488:
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		return string(utf16.Decode(u))
	case 1202:
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i+1])<<8 | uint16(b[2*i])
		}
		return string(utf16.Decode(u))
	case 500:
		return fromEBCDIC(b)
	}
	if m := charmaps[ccsid]; m != nil {
		if s, err := m.NewDecoder().Bytes(b); err == nil {
			return string(s)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return string([]rune(string(b)))
}

func unreadable(ccsid int) error {
	return fmt.Errorf("db2: the database's text is in character set %d (its CCSID), which this client does not read: it reads Unicode's (1208, 1200), ISO 8859's and Windows's", ccsid)
}
