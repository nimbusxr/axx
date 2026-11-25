package demo

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Kind is a direction the pack knows: what it is written like, what it does,
// and what is wrong with a use of it.
type Kind struct {
	// Name is its first word. "lasts" is the one written as a time alone.
	Name string
	// Usage shows it as an author writes it; Doc says what it does, in a
	// film and before an audience.
	Usage string
	Doc   string
	// Text marks a direction that takes the cue's text for itself (a card's
	// title), so that it is no subtitle.
	Text bool
	// Check returns what is wrong with its arguments, or "".
	Check func(args []string) string
}

// Surfaces are what a film can show: the diagram of what the scenario talks
// to, and the screens the scenario drives.
var Surfaces = []string{"diagram", "browser", "phone", "desktop"}

// kinds are the directions, by name.
var kinds = map[string]Kind{}

// Register adds a direction, as the pack adds its own. A pack that builds on
// this one adds its directions from its init.
func Register(k Kind) { kinds[k.Name] = k }

// Kinds are the directions the pack knows, by name.
func Kinds() []Kind {
	out := make([]Kind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func init() {
	Register(Kind{
		Name: "lasts", Usage: "[2s]",
		Doc: "The step below lasts this long in the film, however long it ran. [0s] is a cut: the step runs, and the film shows none of it. Before an audience it does nothing.",
		Check: func(args []string) string {
			if len(args) != 1 {
				return "a time alone says how long the step below lasts in the film, like [2s]: it takes nothing else"
			}
			return ""
		},
	})
	Register(Kind{
		Name: "speed", Usage: "[speed 4x]",
		Doc: "The step below plays this many times as fast: [speed 4x] faster, [speed 0.5x] slower. Before an audience it does nothing.",
		Check: func(args []string) string {
			if len(args) != 1 {
				return "speed takes how many times as fast, like [speed 4x] or [speed 0.5x]"
			}
			if _, err := parseSpeed(args[0]); err != nil {
				return err.Error()
			}
			return ""
		},
	})
	Register(Kind{
		Name: "pause", Usage: "[pause 2s]",
		Doc: "The picture holds this long after the step below. Before an audience it does nothing: the presenter decides when to go on.",
		Check: func(args []string) string {
			if len(args) > 1 {
				return "pause takes one time, like [pause 2s]"
			}
			if len(args) == 1 {
				if _, err := parseDuration(args[0]); err != nil {
					return err.Error()
				}
			}
			return ""
		},
	})
	Register(Kind{
		Name: "cut", Usage: "[cut]",
		Doc:   "The camera stops before the step below: the steps run, and the film shows none of them until [action].",
		Check: noArgs("cut"),
	})
	Register(Kind{
		Name: "action", Usage: "[action]",
		Doc:   "The camera starts again with the step below, after a [cut].",
		Check: noArgs("action"),
	})
	Register(Kind{
		Name: "card", Usage: "[card 3s] A title", Text: true,
		Doc: "A card with the cue's text fills the picture before the step below, for this long (3 seconds when it says no time). Before an audience it stays until the presenter goes on.",
		Check: func(args []string) string {
			if len(args) > 1 {
				return "card takes one time, like [card 3s], and its title after the bracket"
			}
			if len(args) == 1 {
				if _, err := parseDuration(args[0]); err != nil {
					return err.Error()
				}
			}
			return ""
		},
	})
	Register(Kind{
		Name: "show", Usage: "[show browser + diagram]",
		Doc: "What the picture shows from the step below on: " + strings.Join(Surfaces, ", ") + ", two of them side by side with a + between, or auto, which follows the steps again.",
		Check: func(args []string) string {
			names := showSurfaces(args)
			if len(names) == 0 || len(names) > 2 {
				return "show takes what to show: " + strings.Join(Surfaces, ", ") + ", two of them like [show browser + diagram], or auto"
			}
			for _, n := range names {
				if n == "auto" && len(names) == 1 {
					continue
				}
				if !slices.Contains(Surfaces, n) {
					return fmt.Sprintf("show does not know %q: it shows %s, or auto", n, strings.Join(Surfaces, ", "))
				}
			}
			return ""
		},
	})
}

func init() {
	Register(Kind{
		Name: "diagram", Usage: "[diagram checkout]",
		Doc: "The diagram the picture shows from the step below on: one of the project's (packs.demo.diagrams), by its name.",
		Check: func(args []string) string {
			if len(args) != 1 {
				return "diagram takes the name of one of the project's diagrams, like [diagram checkout]"
			}
			return ""
		},
	})
	Register(Kind{
		Name: "as", Usage: "[as courier]",
		Doc: "Who the scenario is, from the step below on: a box of the diagram, which what the scenario sends then comes from. It holds until the cue that goes to another diagram.",
		Check: func(args []string) string {
			if len(args) != 1 {
				return "as takes the name of a box of the diagram, like [as courier]"
			}
			return ""
		},
	})
	Register(Kind{
		Name: "flow", Usage: "[parcels --> addresses]",
		Doc: "Lights a line of the diagram while the step below plays: something one part of the system sends another, which the scenario cannot see. Several in a row, like [shop --> portal --> parcels], light one after the other.",
		Check: func(args []string) string {
			if len(args) < 2 || slices.Contains(args, "") {
				return "an arrow joins two boxes of the diagram, like [parcels --> addresses]"
			}
			return ""
		},
	})
}

func noArgs(name string) func([]string) string {
	return func(args []string) string {
		if len(args) > 0 {
			return fmt.Sprintf("%s takes nothing: write [%s]", name, name)
		}
		return ""
	}
}

// showSurfaces are the surfaces a show direction names: "browser + diagram"
// and "browser+diagram" are two.
func showSurfaces(args []string) []string {
	return strings.FieldsFunc(strings.Join(args, " "), func(r rune) bool { return r == '+' || r == ' ' })
}

// checkDirection returns what is wrong with a direction, or "". Custom are
// the directions a project adds, which its own stage acts on: the pack takes
// them as they are.
func checkDirection(d Direction, known Known) string {
	custom := known.Directions
	if slices.Contains(custom, d.Name) {
		return ""
	}
	k, ok := kinds[d.Name]
	if !ok {
		msg := fmt.Sprintf("there is no direction %q", d.Name)
		if near := nearest(d.Name, custom); near != "" {
			msg += fmt.Sprintf(": did you mean [%s]?", near)
		}
		return msg + " The directions are " + usages(custom) + "; a project adds its own in packs.demo.directions"
	}
	if k.Check != nil {
		if msg := k.Check(d.Args); msg != "" {
			return msg
		}
	}
	// A diagram a cue goes to is one the project has.
	if d.Name == "diagram" && !slices.Contains(known.Diagrams, d.Args[0]) {
		if len(known.Diagrams) == 0 {
			return fmt.Sprintf("the project has no diagram %q: packs.demo.diagrams names none", d.Args[0])
		}
		return fmt.Sprintf("the project has no diagram %q: packs.demo.diagrams has %s", d.Args[0], strings.Join(known.Diagrams, ", "))
	}
	return ""
}

// usages lists the directions as an author writes them.
func usages(custom []string) string {
	var out []string
	for _, k := range Kinds() {
		out = append(out, k.Usage)
	}
	for _, c := range custom {
		out = append(out, "["+c+"]")
	}
	return strings.Join(out, ", ")
}

// nearest is the direction whose name is closest to a word, when one is
// close: a letter or two off.
func nearest(word string, custom []string) string {
	names := append([]string{}, custom...)
	for _, k := range Kinds() {
		if k.Name != "lasts" && k.Name != "flow" {
			names = append(names, k.Name)
		}
	}
	best, bestD := "", 3
	for _, n := range names {
		if d := distance(strings.ToLower(word), n); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

// distance is the number of letters to change, add or drop to make a into b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
