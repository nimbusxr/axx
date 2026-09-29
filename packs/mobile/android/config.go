package mobileandroid

import (
	"fmt"
	"time"

	"github.com/nimbusxr/axx/core"
)

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "devices": {"type": "integer", "minimum": 1, "description": "How many emulators of a device (AVD) run at once: each scenario has one to itself, so it is how many Android scenarios run at once. Default 1."},
    "bootTimeout": {"type": "string", "description": "How long an emulator may take to boot, like 3m. Default 3m."},
    "emulatorArgs": {"type": "array", "items": {"type": "string"}, "description": "More arguments for the emulators axx starts, like [-gpu, host]. On Linux they draw without a GPU (-gpu swiftshader_indirect) unless these name one."}
  }
}`

// Config is the pack's settings, packs.mobile-android in axx.yaml.
type Config struct {
	Devices      int      `json:"devices,omitempty"`
	BootTimeout  string   `json:"bootTimeout,omitempty"`
	EmulatorArgs []string `json:"emulatorArgs,omitempty"`
}

type settings struct {
	devices      int
	bootTimeout  time.Duration
	emulatorArgs []string
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
	out := settings{devices: 1, bootTimeout: 3 * time.Minute, emulatorArgs: c.EmulatorArgs}
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
