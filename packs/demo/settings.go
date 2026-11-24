package demo

import (
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
	// Size is the film's, like 1920x1080, and FPS its frames a second.
	Size string `json:"size,omitempty"`
	FPS  int    `json:"fps,omitempty"`

	// footage is the folder a take's pictures are in: its own.
	footage string
}

// Theme is the look of a project's demos.
type Theme struct {
	// Caller names the box that stands for whoever uses the system: the
	// scenario's side of every exchange.
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
        "caller": {"type": "string", "description": "The name of the box that stands for whoever uses the system, like shop. Default: axx."},
        "steps": {"type": "boolean", "description": "Whether the film says, in a line, which step it shows. Default: true."},
        "colors": {
          "type": "object",
          "additionalProperties": {"type": "string"},
          "description": "The stage's colors, by name: background, surface, text, muted, line, sent, seen, failed, accent."
        },
        "font": {"type": "string", "description": "The font the diagram is drawn in, as CSS names it."}
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
