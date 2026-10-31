package host

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// keptApprovals is how many approved definitions a project keeps: one per
// configuration and profile it runs with, and a few earlier ones.
const keptApprovals = 20

// approvals are the definitions a person approved for a project. They are
// kept outside the project, in the person's own configuration directory,
// where what the agent edits cannot reach them.
type approvals struct {
	path string // "" keeps them in memory only

	mu      sync.Mutex
	project string
	list    []approval // oldest first
}

type approval struct {
	Key  string    `json:"key"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type approvalsFile struct {
	Project  string     `json:"project"`
	Approved []approval `json:"approved"`
}

// ApprovalsFile is where the approvals of the project in dir are kept.
func ApprovalsFile(dir string) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(realPath(dir)))
	return filepath.Join(base, "axx", "host", hex.EncodeToString(sum[:8])+".json"), nil
}

func loadApprovals(path, project string) *approvals {
	a := &approvals{path: path, project: project}
	if path == "" {
		return a
	}
	var f approvalsFile
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &f) == nil {
		a.list = f.Approved
	}
	return a
}

// has reports whether text was approved.
func (a *approvals) has(text string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, x := range a.list {
		if x.Text == text {
			return true
		}
	}
	return false
}

// last is the last definition of key approved, if one was.
func (a *approvals) last(key string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.list) - 1; i >= 0; i-- {
		if a.list[i].Key == key {
			return a.list[i].Text, true
		}
	}
	return "", false
}

// add records text as approved for key, and saves the approvals.
func (a *approvals) add(key, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = slices.DeleteFunc(a.list, func(x approval) bool { return x.Text == text })
	a.list = append(a.list, approval{Key: key, Text: text, At: time.Now().UTC()})
	if len(a.list) > keptApprovals {
		a.list = a.list[len(a.list)-keptApprovals:]
	}
	if a.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(approvalsFile{Project: a.project, Approved: a.list}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(a.path, b, 0o600)
}

// writeFileAtomic writes b to path through a temporary file beside it, so a
// reader never sees half of it.
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
