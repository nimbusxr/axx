package mobileios

import (
	"fmt"
	"time"

	"github.com/nimbusxr/axx/core"
)

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "devices": {"type": "integer", "minimum": 1, "description": "How many simulators of a device run at once: each scenario has one to itself, so it is how many iOS scenarios run at once. Default 1."},
    "bootTimeout": {"type": "string", "description": "How long a simulator may take to boot, like 30m. The simulator axx sets up once, to clone, takes over ten minutes on GitHub's macOS runners; its clones boot in seconds, and are ready once iOS serves apps' requests for notifications (seconds more, or a minute on a busy Mac). Default 20m."},
    "keep": {"type": "boolean", "description": "Keep the simulators booted after the run, for the next run to start at once, instead of making them for each run: in Xcode's set, named axx <device type> (n), where Device Hub lists them. Each scenario still has its simulator to itself, and its app reset. A watched run (axx run --watch) keeps them. Default false."}
  }
}`

// Config is the pack's settings, packs.mobile-ios in axx.yaml.
type Config struct {
	Devices     int    `json:"devices,omitempty"`
	BootTimeout string `json:"bootTimeout,omitempty"`
	Keep        bool   `json:"keep,omitempty"`
}

type settings struct {
	devices     int
	bootTimeout time.Duration
	keep        bool
}

func settingsFor(s *core.Suite) (settings, error) {
	return core.Cached(s, Name+"/settings", func() (settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return settings{}, err
		}
		return parseConfig(c)
	})
}

func parseConfig(c Config) (settings, error) {
	out := settings{devices: 1, bootTimeout: 20 * time.Minute, keep: c.Keep}
	if c.Devices < 0 {
		return out, fmt.Errorf("packs.%s.devices: %d is not a number of devices", Name, c.Devices)
	}
	if c.Devices > 0 {
		out.devices = c.Devices
	}
	if c.BootTimeout != "" {
		d, err := time.ParseDuration(c.BootTimeout)
		if err != nil || d <= 0 {
			return out, fmt.Errorf("packs.%s.bootTimeout: %q is not a duration like 3m", Name, c.BootTimeout)
		}
		out.bootTimeout = d
	}
	return out, nil
}
