package webcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
)

// A run pauses before the steps `axx run --pause-at` names, and, while
// watching, a failed scenario can pause too: the page shows in Playwright's
// Inspector, which resumes the run, steps through its actions, picks
// elements and records what people do as steps. While a scenario is
// paused, an IDE can also highlight on the page what a step names, run
// steps in the scenario, and read the steps recorded: at a local address the
// run announces to it ("[AXX-IDE] paused url=...").

// pauseAtStep pauses the scenario before its step, if the run pauses
// there: on the current page, or, when the scenario has none yet, as soon
// as the step opens one.
func pauseAtStep(sc *core.Scenario, step *core.StepInfo) {
	if !sc.Suite().PausesAt(sc.URI, step.Line) {
		return
	}
	st := scenarioPages.Of(sc)
	st.mu.Lock()
	s := st.current
	if s == nil {
		st.pauseOnOpen = true
	}
	st.mu.Unlock()
	if s != nil {
		s.pause(sc, fmt.Sprintf("%s:%d", sc.URI, step.Line))
	}
}

// pause shows the session's current page in the Inspector, and waits until
// it is resumed there, its page closes or the run is interrupted. where is
// the step it pauses before, or what it pauses for.
func (s *session) pause(sc *core.Scenario, where string) {
	pg, err := s.page()
	if err != nil {
		return
	}
	sc.Suite().Logger().Warn(fmt.Sprintf("the scenario %q is paused (%s) in Playwright's Inspector: resume it there to go on", sc.Name, where))
	if ps, err := pauseServerFor(sc.Suite()); err == nil {
		ps.paused(sc, s)
		sc.Suite().Announce("paused", "url", ps.url, "location", where, "recording", recordingPath(sc.Suite()))
		defer func() {
			ps.paused(nil, nil)
			sc.Suite().Announce("resumed", "url", ps.url)
		}()
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sc.Context().Done():
			if d, err := s.ctx.Debugger(); err == nil {
				_ = d.Resume()
			}
		case <-done:
		}
	}()
	_ = pg.Pause()
	// A pause that ends with its page leaves the context without timeouts.
	s.ctx.SetDefaultTimeout(float64(actionTimeout.Milliseconds()))
	s.ctx.SetDefaultNavigationTimeout(float64(navigationTimeout.Milliseconds()))
}

// recordingPath is where the Inspector's recorder writes, while a run lasts,
// the steps it records ($AXX_WEB_RECORDING for the driver).
func recordingPath(s *core.Suite) string {
	return filepath.Join(s.ProjectDir(), ".axx", "web", "recording.txt")
}

// pauseServer takes an IDE's requests about the paused scenario, at a
// local address with a secret path.
type pauseServer struct {
	url   string
	suite *core.Suite

	mu      sync.Mutex
	sc      *core.Scenario
	session *session
}

func pauseServerFor(suite *core.Suite) (*pauseServer, error) {
	return core.Cached(suite, Name+"/pause-server", func() (*pauseServer, error) {
		var lc net.ListenConfig
		ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		secret := make([]byte, 16)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		base := "/" + hex.EncodeToString(secret) + "/"
		ps := &pauseServer{url: "http://" + ln.Addr().String() + base, suite: suite}
		mux := http.NewServeMux()
		mux.HandleFunc("POST "+base+"highlight", ps.handle(ps.highlight))
		mux.HandleFunc("POST "+base+"run", ps.handle(ps.run))
		mux.HandleFunc("GET "+base+"recorded", ps.handle(func(*core.Scenario, *session, string) (string, error) { return recorded(suite), nil }))
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go srv.Serve(ln) //nolint:errcheck
		suite.OnClose(func(ctx context.Context) error { return srv.Shutdown(ctx) })
		return ps, nil
	})
}

// paused sets the paused scenario and session; nil when none is.
func (ps *pauseServer) paused(sc *core.Scenario, s *session) {
	ps.mu.Lock()
	ps.sc, ps.session = sc, s
	ps.mu.Unlock()
}

// errNotFound answers 404: what a request names is not on the page.
var errNotFound = errors.New("not found")

func (ps *pauseServer) handle(fn func(sc *core.Scenario, s *session, body string) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		ps.mu.Lock()
		sc, s := ps.sc, ps.session
		ps.mu.Unlock()
		if sc == nil && r.Method != http.MethodGet {
			http.Error(w, "no scenario is paused", http.StatusConflict)
			return
		}
		out, err := fn(sc, s, string(body))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		switch {
		case errors.Is(err, errNotFound):
			w.WriteHeader(http.StatusNotFound)
			out = strings.TrimSpace(strings.TrimPrefix(err.Error(), errNotFound.Error()+": "))
		case err != nil:
			w.WriteHeader(http.StatusUnprocessableEntity)
			out = err.Error()
		}
		_, _ = io.WriteString(w, out)
	}
}

// elementOfStep is the element a step names: `the "Register" button`, or
// what the pointer is moved over.
var elementOfStep = regexp.MustCompile(`the ("(?:[^"\\]|\\.)*") (buttons?|fields?|checkbox(?:es)?|options?|links?|tabs?|menu items?|elements?)\b|moved over ("(?:[^"\\]|\\.)*")`)

// highlight highlights on the paused page the element a step names, and
// says how many there are.
func (ps *pauseServer) highlight(_ *core.Scenario, s *session, step string) (string, error) {
	m := elementOfStep.FindStringSubmatch(step)
	if m == nil {
		return "", fmt.Errorf("%w: the step names no element", errNotFound)
	}
	k, quoted := pointable, m[3]
	if m[1] != "" {
		k, quoted = kinds[m[2]], m[1]
	}
	name, err := strconv.Unquote(quoted)
	if err != nil {
		return "", err
	}
	if pg, err := s.page(); err == nil {
		_ = pg.HideHighlight()
	}
	loc, n, err := s.count(k, name)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("%w: %s", errNotFound, s.none(k, name).Error())
	}
	_ = loc.Highlight()
	if n > 1 {
		return fmt.Sprintf("%d %s are %s; a step needs exactly one", n, k.plural, named(name)), nil
	}
	return fmt.Sprintf("the %s %s", k.noun, named(name)), nil
}

// run runs a step, with its table rows if it has some, in the paused
// scenario.
func (ps *pauseServer) run(sc *core.Scenario, _ *session, step string) (string, error) {
	lines := strings.Split(strings.TrimSpace(step), "\n")
	var table *core.Table
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			continue
		}
		if table == nil {
			table = &core.Table{}
		}
		var row []string
		for _, cell := range strings.Split(strings.Trim(l, "|"), "|") {
			row = append(row, strings.TrimSpace(cell))
		}
		table.Rows = append(table.Rows, row)
	}
	if err := ps.suite.Invoke(sc, lines[0], table, nil); err != nil {
		return "", err
	}
	return "passed", nil
}

// recorded are the steps the Inspector recorded in the run, without the
// recorder's first line, which says what they are.
func recorded(s *core.Suite) string {
	b, err := os.ReadFile(recordingPath(s))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "#") {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// pauseWhereFailed pauses a failed scenario, at the step that failed.
func pauseWhereFailed(sc *core.Scenario, st *pages) {
	st.mu.Lock()
	s, last := st.current, st.lastStep
	st.mu.Unlock()
	if s == nil {
		return
	}
	where := fmt.Sprintf("%s:%d", sc.URI, sc.Line)
	if last != nil {
		// The failed step's group again: the Inspector shows its line.
		failed := *last
		failed.Text += " (paused where it failed)"
		s.startGroup(sc, &failed)
		defer s.endGroup()
		where = fmt.Sprintf("%s:%d", sc.URI, last.Line)
	}
	s.pause(sc, where)
}
