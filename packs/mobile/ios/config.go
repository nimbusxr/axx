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
    "bootTimeout": {"type": "string", "description": "How long a simulator may take to boot, like 10m: an attempt that stalls is booted again, within it. Default 5m."}
  }
}`

// Config is the pack's settings, packs.mobile-ios in axx.yaml.
type Config struct {
	Devices     int    `json:"devices,omitempty"`
	BootTimeout string `json:"bootTimeout,omitempty"`
}

type settings struct {
	devices     int
	bootTimeout time.Duration
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
	out := settings{devices: 1, bootTimeout: 5 * time.Minute}
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
