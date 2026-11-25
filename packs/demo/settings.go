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
	Theme      Theme    `json:"theme,omitzero"`
	// Scenes and Styles are the project's own scripts and stylesheets for
	// the stage, loaded after the pack's.
	Scenes []string `json:"scenes,omitempty"`
	Styles []string `json:"styles,omitempty"`
	// Diagrams are the project's diagrams of its system, by name: files of
	// Mermaid flowcharts or sequence diagrams. A demo starts on the first, and a cue goes to
	// another ([diagram name]).
	Diagrams Diagrams `json:"diagrams,omitzero"`
	// Soundtrack is how a film's sound is made: nothing, without it.
	Soundtrack Soundtrack `json:"soundtrack,omitzero"`
	// Size is the film's, like 1920x1080, and FPS its frames a second.
	Size string `json:"size,omitempty"`
	FPS  int    `json:"fps,omitempty"`

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
	// Steps is whether the film says, in a line, which step it shows.
	Steps *bool `json:"steps,omitempty"`
	// Colors are the stage's colors, by name: background, surface, text,
	// muted, line, sent, seen, failed, accent.
	Colors map[string]string `json:"colors,omitempty"`
	// Font is the font the diagram is drawn in.
	Font string `json:"font,omitempty"`
}

// size is the page the film is taken of, in CSS pixels, and how many of the
// film's pixels each is: the page is always 720 high, so that a demo looks
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
    "theme": {
      "type": "object",
      "additionalProperties": false,
      "description": "The look of the project's demos.",
      "properties": {
        "caller": {"type": "string", "description": "A name for whoever uses the system, like shop: the diagram then has a box of that name, which what the scenario sends comes from. Without it there is no such box, and what is sent comes in from the edge of the picture."},
        "steps": {"type": "boolean", "description": "Whether the film says, in a line, which step it shows. Default: true."},
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
      "description": "How a film's sound is made, once its picture is: by a command of the project's. Axx makes no sound itself.",
      "properties": {
        "command": {"type": "string", "description": "A command of the project's, run through the shell in axx.yaml's folder for each film. It reads the film's subtitles with their times ($AXX_DEMO_SUBTITLES, a .srt file) and writes the film's sound to $AXX_DEMO_SOUNDTRACK, a WAV file as long as the film ($AXX_DEMO_SECONDS); $AXX_DEMO_FILM is the film without sound. A model that speaks, or anything else that makes sound."}
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
    "size": {"type": "string", "pattern": "^[0-9]+x[0-9]+$", "description": "The film's size, like 1920x1080 (the default)."},
    "fps": {"type": "integer", "minimum": 1, "maximum": 60, "description": "The film's frames a second. Default: 30."}
  }
}`
