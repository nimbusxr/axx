package demo

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/video"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// The film is the stage, moment after moment. The stage is a page that draws
// the picture of any moment of a timeline (stage/stage.js: seek); the film
// asks it for each frame's, takes a picture of the page, and encodes the
// pictures. Nothing depends on how fast the page or the machine is: the
// film's clock is the timeline's.

//go:embed stage
var stageFiles embed.FS

// Staged is what the stage plays: a take, its timeline and the project's look.
type Staged struct {
	Take     *Take     `json:"take"`
	Timeline *Timeline `json:"timeline"`
	Theme    Theme     `json:"theme"`
	// Mark is a word the stage shows in a corner: "recorded", where a live
	// show plays a take.
	Mark string `json:"mark,omitempty"`
	// Live is the state of a show before an audience; nil for a film.
	Live *Live `json:"live,omitempty"`
	// Diagrams are the project's diagrams, the first the one a demo starts
	// on. With none, the diagram is of what the scenario talked to.
	Diagrams []Diagram `json:"diagrams,omitempty"`
}

// stageServer serves the stage to a page, on this machine only: the pack's
// files, what the stage plays, and the project's own scenes and styles.
type stageServer struct {
	URL   string
	close func()
}

func (s *stageServer) Close() { s.close() }

// serveStage starts the stage's server. What it plays comes from staged each
// time the page asks, so a show can change it.
func serveStage(projectDir string, st Settings, staged func() *Staged, sh *show) (*stageServer, error) {
	files, _ := fs.Sub(stageFiles, "stage")
	mux := http.NewServeMux()
	// A show's pages follow it as it changes, and take the presenter's word.
	prefix := ""
	if sh != nil {
		// The address of a show has a word only its pages know, so that
		// nothing else on this machine moves it.
		token := make([]byte, 12)
		_, _ = rand.Read(token)
		prefix = "/" + hex.EncodeToString(token)
		mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			seen := -1
			for {
				version, changed := sh.watch()
				if version != seen {
					seen = version
					fmt.Fprintf(w, "data: %d\n\n", version)
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
					continue
				}
				select {
				case <-changed:
				case <-r.Context().Done():
					return
				case <-sh.quit:
					return
				}
			}
		})
		mux.HandleFunc("POST /control/{word}", func(w http.ResponseWriter, r *http.Request) {
			sh.say(r.PathValue("word"))
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("/presenter", func(w http.ResponseWriter, _ *http.Request) {
			page, _ := fs.ReadFile(files, "presenter.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		})
	}
	mux.HandleFunc("/demo.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(staged())
	})
	// Mermaid's library, which reads the project's diagrams.
	mux.HandleFunc("/mermaid.min.js", func(w http.ResponseWriter, r *http.Request) {
		if st.mermaid == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, st.mermaid)
	})
	// The pictures of the take's screens, from the demo's folder.
	mux.HandleFunc("/footage/", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/footage/")
		for _, shot := range staged().Take.Footage {
			if shot.File == rel {
				http.ServeFile(w, r, filepath.Join(st.footage, filepath.FromSlash(rel)))
				return
			}
		}
		http.NotFound(w, r)
	})
	// The project's own files, by the paths axx.yaml names them with.
	mux.HandleFunc("/project/", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/project/")
		for _, allowed := range append(append([]string{}, st.Scenes...), st.Styles...) {
			if filepath.ToSlash(filepath.Clean(allowed)) == rel {
				http.ServeFile(w, r, filepath.Join(projectDir, filepath.FromSlash(rel)))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/stage.html" {
			http.FileServerFS(files).ServeHTTP(w, r)
			return
		}
		page, _ := fs.ReadFile(files, "stage.html")
		var styles, scenes strings.Builder
		for _, s := range st.Styles {
			fmt.Fprintf(&styles, `<link rel="stylesheet" href="project/%s">`+"\n", filepath.ToSlash(filepath.Clean(s)))
		}
		for _, s := range st.Scenes {
			fmt.Fprintf(&scenes, `<script src="project/%s"></script>`+"\n", filepath.ToSlash(filepath.Clean(s)))
		}
		out := strings.Replace(string(page), "<!--theme-->", styles.String(), 1)
		if st.mermaid != "" {
			out = strings.Replace(out, "<!--mermaid-->", `<script src="mermaid.min.js"></script>`, 1)
		}
		out = strings.Replace(out, "<!--scenes-->", scenes.String(), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(out))
	})
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cannot serve the demo's stage on this machine: %w", err)
	}
	var handler http.Handler = mux
	if prefix != "" {
		handler = http.StripPrefix(prefix, mux)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(l) }()
	return &stageServer{
		URL:   "http://" + l.Addr().String() + prefix + "/",
		close: func() { _ = srv.Close() },
	}, nil
}

// studio is the stage open in a page of a browser of its own, to take
// pictures of.
type studio struct {
	page          playwright.Page
	width, height int
	scale         float64
	close         func()
}

// openStudio opens the stage with what it plays.
func openStudio(ctx context.Context, projectDir string, st Settings, staged *Staged) (*studio, error) {
	srv, err := serveStage(projectDir, st, func() *Staged { return staged }, nil)
	if err != nil {
		return nil, err
	}
	// A browser of its own, for the stage's page: it ends with the studio.
	suite := core.NewSuite(core.SuiteOptions{ProjectDir: projectDir})
	s := &studio{}
	s.close = func() {
		if s.page != nil {
			_ = s.page.Close()
		}
		_ = suite.Close(context.WithoutCancel(ctx))
		srv.Close()
	}
	s.width, s.height, s.scale = st.size()
	if s.page, err = webcore.OwnPage(suite, s.width, s.height, s.scale); err != nil {
		s.close()
		return nil, fmt.Errorf("the film is made in a browser, which did not start: %w", err)
	}
	if _, err := s.page.Goto(srv.URL); err != nil {
		s.close()
		return nil, fmt.Errorf("the demo's stage did not open: %w", err)
	}
	if _, err := s.page.WaitForFunction("() => window.axxReady === true", nil); err != nil {
		err = fmt.Errorf("the demo's stage did not start: %w", stageError(s.page, err))
		s.close()
		return nil, err
	}
	// What the stage could not make sense of: a diagram that is no Mermaid
	// flowchart, a cue that names a box or a line its diagram has not.
	if said, err := s.page.Evaluate(`() => (axxStage.problems || []).join("\n")`); err == nil {
		if problems, _ := said.(string); problems != "" {
			s.close()
			return nil, fmt.Errorf("the demo's diagrams have mistakes:\n  %s", strings.ReplaceAll(problems, "\n", "\n  "))
		}
	}
	return s, nil
}

// seek moves the stage to a moment, and reports whether its picture changed.
func (s *studio) seek(at time.Duration) (bool, error) {
	// A frame is a picture only where the stage changed: a still stretch is
	// one frame that lasts.
	const seek = `(t) => {
		axxStage.seek(t);
		const now = document.getElementById('stage').innerHTML;
		const changed = now !== window.__axxLast;
		window.__axxLast = now;
		return changed;
	}`
	changed, err := s.page.Evaluate(seek, at.Seconds())
	if err != nil {
		return false, fmt.Errorf("the demo's stage failed at %s: %w", at.Round(time.Millisecond), err)
	}
	c, _ := changed.(bool)
	return c, nil
}

// shot is the picture of the stage as it stands.
func (s *studio) shot() (image.Image, error) {
	shot, err := s.page.Screenshot(playwright.PageScreenshotOptions{Type: playwright.ScreenshotTypePng})
	if err != nil {
		return nil, fmt.Errorf("no picture of the stage: %w", err)
	}
	return png.Decode(bytes.NewReader(shot))
}

// Still is the picture of a demo at a moment of its timeline.
func Still(ctx context.Context, projectDir string, st Settings, staged *Staged, at time.Duration) (image.Image, error) {
	s, err := openStudio(ctx, projectDir, st, staged)
	if err != nil {
		return nil, err
	}
	defer s.close()
	if _, err := s.seek(at); err != nil {
		return nil, err
	}
	return s.shot()
}

// Film makes the film of a take with its timeline, into the file at out.
// Progress is told how far it is, from 0 to 1.
func Film(ctx context.Context, projectDir string, st Settings, staged *Staged, out string, progress func(float64)) error {
	return FilmOf(ctx, projectDir, st, []*Staged{staged}, nil, out, progress)
}

// FilmOf makes one film of several takes, one after another: the rows of a
// Scenario Outline, each a chapter of the film, with its title. A film of
// one take has a chapter for each of its subtitles.
func FilmOf(ctx context.Context, projectDir string, st Settings, parts []*Staged, titles []string, out string, progress func(float64)) error {
	clip, chapters, err := shoot(ctx, projectDir, st, parts, titles, progress)
	if err != nil {
		return err
	}
	return writeFilm(out, clip, nil, chapters)
}

// writeFilm writes a film's picture, with its sound when it has one.
func writeFilm(out string, clip video.Clip, sound *video.Sound, chapters []video.Chapter) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := video.WriteMP4Sound(&buf, []video.Clip{clip}, sound, chapters...); err != nil {
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// shoot films the stage for each take, one after another, and returns the
// picture and its chapters.
func shoot(ctx context.Context, projectDir string, st Settings, parts []*Staged, titles []string, progress func(float64)) (video.Clip, []video.Chapter, error) {
	var none video.Clip
	total := 0.0
	for _, p := range parts {
		total += p.Timeline.Duration
	}
	if total <= 0 {
		return none, nil, errors.New("the demo shows nothing: its camera is off for every step ([cut] with no [action] after it)")
	}
	width, height, scale := st.size()
	fps := st.fps()
	enc, err := video.NewEncoderOf(int(float64(width)*scale), int(float64(height)*scale), float64(fps), video.ScreenQuality)
	if err != nil {
		return none, nil, err
	}
	defer enc.Close()
	var chapters []video.Chapter
	offset := time.Duration(0)
	for i, part := range parts {
		tl := part.Timeline
		if tl.Duration <= 0 {
			continue
		}
		if len(parts) > 1 {
			chapters = append(chapters, video.Chapter{Title: titles[i], At: offset})
		} else {
			for _, s := range tl.Subtitles {
				chapters = append(chapters, video.Chapter{Title: s.Text, At: time.Duration(s.Start * float64(time.Second))})
			}
		}
		s, err := openStudio(ctx, projectDir, st, part)
		if err != nil {
			return none, nil, err
		}
		frames := int(tl.Duration*float64(fps)) + 1
		for n := range frames {
			if err := ctx.Err(); err != nil {
				s.close()
				return none, nil, err
			}
			at := time.Duration(float64(n) / float64(fps) * float64(time.Second))
			changed, err := s.seek(at)
			if err != nil {
				s.close()
				return none, nil, err
			}
			if !changed && n > 0 {
				continue
			}
			img, err := s.shot()
			if err != nil {
				s.close()
				return none, nil, err
			}
			if err := enc.Encode(opaque(img), offset+at); err != nil {
				s.close()
				return none, nil, err
			}
			if progress != nil {
				progress((offset.Seconds() + at.Seconds()) / total)
			}
		}
		s.close()
		offset += time.Duration(tl.Duration * float64(time.Second))
	}
	return enc.Finish(offset), chapters, nil
}

// opaque is a picture as the encoder takes it quickest.
func opaque(img image.Image) image.Image {
	if n, ok := img.(*image.NRGBA); ok {
		return &image.RGBA{Pix: n.Pix, Stride: n.Stride, Rect: n.Rect}
	}
	return img
}

// stageError adds what the page said to an error of the stage: a mistake in
// a project's own scene shows there.
func stageError(page playwright.Page, err error) error {
	said, eerr := page.Evaluate(`() => window.__axxErrors ? window.__axxErrors.join("; ") : ""`)
	if s, _ := said.(string); eerr == nil && s != "" {
		return fmt.Errorf("%w (%s)", err, s)
	}
	return err
}
