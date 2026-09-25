package config

import (
	"bytes"
	"encoding/json"
	"runtime"
)

func indentJSON(dst *bytes.Buffer, src []byte) error {
	return json.Indent(dst, src, "", "  ")
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac OS X"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}
