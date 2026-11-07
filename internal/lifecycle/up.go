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

// UpTold is what the services `axx up` keeps running in dir told axx: none
// when no supervisor runs.
func UpTold(dir string) ([]ToldValues, error) {
	services, ok := upServices(dir)
	if !ok {
		return nil, nil
	}
	files := make([]Told, len(services))
	for i, name := range services {
		files[i] = Told{Who: "service " + name, Service: name, Path: PropertiesFile(PropertiesDir(dir), name)}
	}
	return ReadTold(files)
}

// upServices are the services the supervisor of `axx up` in dir keeps, while
// it runs.
func upServices(dir string) ([]string, bool) {
	b, err := os.ReadFile(UpFile(dir))
	if err != nil {
		return nil, false
	}
	var st struct {
		PID      int      `json:"pid"`
		Services []string `json:"services"`
	}
	if json.Unmarshal(b, &st) != nil || !ProcessAlive(st.PID) {
		return nil, false
	}
	return st.Services, true
}
