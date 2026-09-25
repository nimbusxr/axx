package runner

import (
	b64 "encoding/base64"
	"strings"
)

func isText(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return strings.HasPrefix(mt, "text/") || strings.Contains(mt, "json") || strings.Contains(mt, "xml") ||
		strings.Contains(mt, "yaml") || strings.HasSuffix(mt, "+plain")
}

func base64(b []byte) string { return b64.StdEncoding.EncodeToString(b) }
