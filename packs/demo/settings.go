package demo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Settings are the pack's section of axx.yaml, packs.demo.
type Settings struct {
	// Directions are the project's own directions, which its stage acts on.
	Directions []string `json:"directions,omitempty"`
	// Surfaces are the project's own surfaces, which its scenes draw:
	// [show terminal] shows the scene its script registers under that name.
	Surfaces []string `json:"surfaces,omitempty"`
	Theme    Theme    `json:"theme,omitzero"`
	// Scenes and Styles are the project's own scripts and stylesheets for
	// the stage, loaded after the pack's.
	Scenes []string `json:"scenes,omitempty"`
	Styles []string `json:"styles,omitempty"`
	// Diagrams are the project's diagrams of its system, by name: files of
	// Mermaid flowcharts or sequence diagrams. A demo starts on the first, and a cue goes to
	// another ([diagram name]).
	Diagrams Diagrams `json:"diagrams,omitzero"`
	// Soundtrack is how a video's sound is made: nothing, without it.
	Soundtrack Soundtrack `json:"soundtrack,omitzero"`
	// Size is the video's, like 1920x1080, and FPS its frames a second.
	Size string `json:"size,omitempty"`
	FPS  int    `json:"fps,omitempty"`
	// Pace is how fast the project's demo videos go, where it differs from
	// the pack's: seconds, each 0 where the project says nothing.
	Pace struct {
		Read     float64 `json:"read,omitempty"`
		Word     float64 `json:"word,omitempty"`
		Exchange float64 `json:"exchange,omitempty"`
		Check    float64 `json:"check,omitempty"`
		Screen   float64 `json:"screen,omitempty"`
		Card     float64 `json:"card,omitempty"`
		End      float64 `json:"end,omitempty"`
	} `json:"pace,omitzero"`

	// footage is the folder a take's pictures are in: its own.
	footage string
	// mermaid is Mermaid's library on this machine, for a project with
	// diagrams.
	mermaid string
}

// Diagrams are a project's diagrams: each one's file by its name, and the
// names in the order axx.yaml has them.
type Diagrams struct {
	Names []string
	Files map[string]string
}

// UnmarshalJSON reads the diagrams as a mapping of name to file, keeping its
// order.
func (d *Diagrams) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return errors.New("packs.demo.diagrams is a mapping of a diagram's name to its file")
	}
	d.Files = map[string]string{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		var file string
		if err := dec.Decode(&file); err != nil {
			return fmt.Errorf("packs.demo.diagrams.%v is a file's path: %w", key, err)
		}
		name, _ := key.(string)
		d.Names = append(d.Names, name)
		d.Files[name] = file
	}
	return nil
}

// IsZero reports whether the project has no diagrams.
func (d Diagrams) IsZero() bool { return len(d.Names) == 0 }

// MarshalJSON writes the diagrams as they are read.
func (d Diagrams) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, name := range d.Names {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(name)
		v, _ := json.Marshal(d.Files[name])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Theme is the look of a project's demos.
type Theme struct {
	// Caller names whoever uses the system, and gives them a box in the
	// diagram: the scenario's side of every exchange. Without it there is
	// no such box.
	Caller string `json:"caller,omitempty"`
	// Steps is whether the video says, in a line, which step it shows.
	Steps *bool `json:"steps,omitempty"`
	// Colors are the stage's colors, by name: background, surface, text,
	// muted, line, sent, seen, failed, accent.
	Colors map[string]string `json:"colors,omitempty"`
	// Font is the font the diagram is drawn in.
	Font string `json:"font,omitempty"`
}

// size is the page the video is taken of, in CSS pixels, and how many of the
// video's pixels each is: the page is always 720 high, so that a demo looks
// the same at every size.
func (s Settings) size() (width, height int, scale float64) {
	w, h := 1920, 1080
	if a, b, ok := strings.Cut(s.Size, "x"); ok {
		if x, err := strconv.Atoi(a); err == nil && x >= 320 {
			if y, err := strconv.Atoi(b); err == nil && y >= 240 {
				w, h = x, y
			}
		}
	}
	scale = float64(h) / 720
	return int(float64(w)/scale + 0.5), 720, scale
}

// pace is the pace of the project's demo videos: the pack's, with what the
// project sets of its own.
func (s Settings) pace() Pace {
	p := DefaultPace
	for _, v := range []struct {
		set float64
		to  *float64
	}{{s.Pace.Read, &p.Read}, {s.Pace.Word, &p.Word}, {s.Pace.Exchange, &p.Exchange}, {s.Pace.Check, &p.Check}, {s.Pace.Screen, &p.Screen}, {s.Pace.Card, &p.Card}, {s.Pace.End, &p.End}} {
		if v.set > 0 {
			*v.to = v.set
		}
	}
	p.Most = max(p.Most, p.Exchange)
	return p
}

func (s Settings) fps() int {
	if s.FPS >= 1 && s.FPS <= 60 {
		return s.FPS
	}
	return 30
}

// configSchema is the pack's section of axx.yaml, packs.demo.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "directions": {
      "type": "array", "items": {"type": "string", "pattern": "^[a-z][a-z0-9-]*$"},
      "description": "The project's own directions, like confetti for [confetti]: the demo pack takes them as they are, and the project's scenes act on them."
    },
    "surfaces": {
      "type": "array", "items": {"type": "string", "pattern": "^[a-z][a-z0-9-]*$"},
      "description": "The project's own surfaces, like ledger for [show ledger]: each is a scene one of the project's scripts registers under that name (axxStage.scene)."
    },
    "theme": {
      "type": "object",
      "additionalProperties": false,
      "description": "The look of the project's demos.",
      "properties": {
        "caller": {"type": "string", "description": "A name for whoever uses the system, like shop: the diagram then has a box of that name, which what the scenario sends comes from. Without it there is no such box, and what is sent comes in from the edge of the picture."},
        "steps": {"type": "boolean", "description": "Whether the video says, in a line, which step it shows. Default: true."},
        "colors": {
          "type": "object",
          "additionalProperties": {"type": "string"},
          "description": "The stage's colors, by name: background, surface, text, muted, line, sent, seen, failed, accent."
        },
        "font": {"type": "string", "description": "The font the diagram is drawn in, as CSS names it."}
      }
    },
    "diagrams": {
      "type": "object",
      "additionalProperties": {"type": "string"},
      "propertyNames": {"pattern": "^[a-z][a-z0-9-]*$"},
      "description": "The project's diagrams of its system, by name: files of Mermaid flowcharts or sequence diagrams (system: docs/system.mmd), whose boxes and participants are named as the scenarios name what they talk to. A demo's diagram is the first; a cue goes to another with [diagram name]. Axx draws them itself, in characters. Without any, a demo's diagram is of what its scenario talked to."
    },
    "soundtrack": {
      "type": "object",
      "additionalProperties": false,
      "description": "How a video's sound is made, once its picture is: by a command of the project's. Axx makes no sound itself.",
      "properties": {
        "command": {"type": "string", "description": "A command of the project's, run through the shell in axx.yaml's folder for each video. It reads the video's subtitles with their times ($AXX_DEMO_SUBTITLES, a .srt file) and writes the video's sound to $AXX_DEMO_SOUNDTRACK, a WAV file as long as the video ($AXX_DEMO_SECONDS); $AXX_DEMO_VIDEO is the video without sound. A model that speaks, or anything else that makes sound."}
      }
    },
    "scenes": {
      "type": "array", "items": {"type": "string"},
      "description": "Scripts of the project's own for the stage, loaded after the pack's: a scene one registers replaces the pack's of that name."
    },
    "styles": {
      "type": "array", "items": {"type": "string"},
      "description": "Stylesheets of the project's own for the stage, loaded after the pack's."
    },
    "size": {"type": "string", "pattern": "^[0-9]+x[0-9]+$", "description": "The video's size, like 1920x1080 (the default)."},
    "fps": {"type": "integer", "minimum": 1, "maximum": 60, "description": "The video's frames a second. Default: 30."},
    "pace": {
      "type": "object",
      "additionalProperties": false,
      "description": "How fast a demo video goes, in seconds. The defaults are slow enough for someone who reads the subtitles and sees the system for the first time; a video with a voice is tighter, as a line is heard faster than it is read.",
      "properties": {
        "read": {"type": "number", "exclusiveMinimum": 0, "description": "How long a subtitle stays before its words are counted. Default: 2."},
        "word": {"type": "number", "exclusiveMinimum": 0, "description": "What each word of a subtitle adds to how long it stays. Default: 0.42."},
        "exchange": {"type": "number", "exclusiveMinimum": 0, "description": "How long a step lasts for each thing it sent or saw. Default: 3.4."},
        "check": {"type": "number", "exclusiveMinimum": 0, "description": "How long a step lasts that sent nothing: a check. Default: 1.8."},
        "screen": {"type": "number", "exclusiveMinimum": 0, "description": "The least a step on a screen lasts. Default: 1.8."},
        "card": {"type": "number", "exclusiveMinimum": 0, "description": "How long a card stays that says no time. Default: 3."},
        "end": {"type": "number", "exclusiveMinimum": 0, "description": "How long the last picture stays. Default: 2.5."}
      }
    }
  }
}`
