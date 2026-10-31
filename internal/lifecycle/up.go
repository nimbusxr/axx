package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// UpFile is where the supervisor `axx up` starts records the services it
// keeps running for the project in dir.
func UpFile(dir string) string { return filepath.Join(dir, ".axx", "run", "up.json") }

// UpRunning reports whether that supervisor still runs.
func UpRunning(dir string) bool {
	b, err := os.ReadFile(UpFile(dir))
	if err != nil {
		return false
	}
	var st struct {
		PID int `json:"pid"`
	}
	return json.Unmarshal(b, &st) == nil && ProcessAlive(st.PID)
}
