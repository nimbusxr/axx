package host

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Protocol is the version of what a run and the host send each other: one
// JSON object per line, over one TCP connection per run.
const Protocol = 1

// Announcement is what a host writes to .axx/host.json for the runs of its
// project to find it.
type Announcement struct {
	Addr    string    `json:"addr"`
	Token   string    `json:"token"`
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Started time.Time `json:"started"`
}

// AnnouncementFile is where the host serving the project in dir announces
// itself.
func AnnouncementFile(dir string) string { return filepath.Join(dir, ".axx", "host.json") }

// Find returns the host the project in dir has announced, or nil when it
// has none. Whether it still answers is for [Open] to find out.
func Find(dir string) (*Announcement, error) {
	b, err := os.ReadFile(AnnouncementFile(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, hostErr(CodeRefused, "cannot read %s: %v", AnnouncementFile(dir), err).
			WithHint("the axx host writes it for runs as the same user; run axx as that user, or stop the host to start services here")
	}
	var a Announcement
	if err := json.Unmarshal(b, &a); err != nil || a.Addr == "" || a.Token == "" {
		return nil, nil //nolint:nilerr // half written, or not a host's: no host
	}
	return &a, nil
}

// Request is the first line a run sends: the services it needs, and the
// configuration it loaded them from.
type Request struct {
	Protocol int    `json:"protocol"`
	Token    string `json:"token"`
	// Command is what the run is, for the person: "axx run --compact features/orders".
	Command string `json:"command"`
	// IDE is the editor that started the run (AXX_IDE): an editor shows its
	// own runs, and the host's others.
	IDE string `json:"ide,omitempty"`
	// Dir is the run's project directory as it sees it: its report's
	// locations are there.
	Dir string `json:"dir,omitempty"`
	// Config is the configuration file the run loaded, relative to the
	// project ("" when it found axx.yaml there itself).
	Config     string            `json:"config,omitempty"`
	Profile    string            `json:"profile,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Settings   []string          `json:"settings,omitempty"`
	// Services are the services to start, with those they depend on; nil
	// starts every enabled one.
	Services []string `json:"services"`
	Attach   []string `json:"attach,omitempty"`
	Debug    []string `json:"debug,omitempty"`
	DebugAll bool     `json:"debugAll,omitempty"`
}

// Message types the host sends a run.
const (
	// MsgHello accepts the run; Report says whether to send its reports.
	MsgHello = "hello"
	// MsgWaiting says what the run waits for: another run, or an approval.
	MsgWaiting = "waiting"
	// MsgOutput is a line of the services' output.
	MsgOutput = "output"
	// MsgStarted says the services are ready.
	MsgStarted = "started"
	// MsgError says the run gets no services: Error says why.
	MsgError = "error"
	// MsgStopped says the services are stopped and cleaned up; Error is set
	// when that failed.
	MsgStopped = "stopped"
)

// Message types a run sends the host after its request.
const (
	// MsgReport is a line of the run's TeamCity report, for an editor.
	MsgReport = "report"
	// MsgResult is how the scenarios ended.
	MsgResult = "result"
	// MsgStop asks the host to stop the services.
	MsgStop = "stop"
)

// Message is one line either way after the request.
type Message struct {
	Type     string   `json:"type"`
	Run      int      `json:"run,omitempty"`
	Report   bool     `json:"report,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Text     string   `json:"text,omitempty"`
	Services []string `json:"services,omitempty"`
	Error    *Failure `json:"error,omitempty"`
	Result   *Result  `json:"result,omitempty"`
}

// Failure is an error the host reports, with its code, hint and exit code.
type Failure struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Exit    int    `json:"exit,omitempty"`
}

// Result counts a run's scenarios by how they ended.
type Result struct {
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Other  int `json:"other"`
}

// lines reads and writes a connection's messages, one JSON object per line.
type lines struct {
	r  *bufio.Reader
	mu sync.Mutex
	w  io.Writer
}

func newLines(rw io.ReadWriter) *lines {
	return &lines{r: bufio.NewReaderSize(rw, 64<<10), w: rw}
}

// maxLine bounds a message: a report line with a long doc string fits.
const maxLine = 16 << 20

func (l *lines) read(v any) error {
	var buf []byte
	for {
		chunk, err := l.r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(buf) > maxLine {
				return errors.New("message too long")
			}
			continue
		}
		if err != nil && (len(buf) == 0 || !errors.Is(err, io.EOF)) {
			return err
		}
		return json.Unmarshal(buf, v)
	}
}

func (l *lines) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(append(b, '\n'))
	return err
}
