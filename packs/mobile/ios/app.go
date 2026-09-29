package mobileios

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// app is an iOS app a scenario registered.
type app struct {
	name        string
	path        string // the app: a simulator build (.app) or it zipped, resolved
	bundleID    string
	device      string // a device type, a simulator's name, or a UDID
	permissions []string
	locale      string // like de-DE
	timezone    string // like Europe/Berlin
	location    *[2]float64
	server      string         // an Appium server of the project's own, or a device farm's
	caps        map[string]any // capability.<name> rows, for that server
}

var (
	localeRE   = regexp.MustCompile(`^([a-z]{2,3})(?:[-_]([A-Z]{2}))?$`)
	bundleRE   = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	serviceRE  = regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	notGranted = map[string]string{
		"notifications": "iOS simulators cannot grant notifications: the app asks, and the scenario answers the dialog (the {word} app's dialog is accepted)",
		"all":           "the permissions name each service the app has, like location, photos",
	}
)

func parseApp(sc *core.Scenario, expand func(string) string, name string, t *core.Table) (*app, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	a := &app{name: name, locale: "en-US", timezone: "UTC", caps: map[string]any{}}
	for _, p := range pairs {
		key, value := p.Key, strings.TrimSpace(expand(p.Value))
		if capName, ok := strings.CutPrefix(key, "capability."); ok {
			a.caps[capName] = capabilityValue(value)
			continue
		}
		switch key {
		case "app":
			if strings.HasSuffix(strings.ToLower(value), ".ipa") {
				return nil, fmt.Errorf("the %s ios app's app %q is for devices: simulators run a simulator build, the .app xcodebuild builds for -sdk iphonesimulator (or it zipped)", name, value)
			}
			path, err := sc.Suite().ResolvePath(value)
			if err != nil {
				return nil, fmt.Errorf("the %s ios app's app %q: %w", name, value, err)
			}
			a.path = path
		case "bundle id":
			if !bundleRE.MatchString(value) {
				return nil, fmt.Errorf("the %s ios app's bundle id %q is not a bundle identifier, like de.parcels.courier", name, value)
			}
			a.bundleID = value
		case "device":
			a.device = value
		case "permissions":
			for _, service := range strings.Split(value, ",") {
				service = strings.ToLower(strings.TrimSpace(service))
				if service == "" {
					continue
				}
				if why, ok := notGranted[service]; ok {
					return nil, fmt.Errorf("the %s ios app's permission %q: %s", name, service, why)
				}
				if !serviceRE.MatchString(service) {
					return nil, fmt.Errorf("the %s ios app's permission %q is not a service the simulator grants, like location or photos (xcrun simctl help privacy lists them)", name, service)
				}
				a.permissions = append(a.permissions, service)
			}
		case "locale":
			if !localeRE.MatchString(value) {
				return nil, fmt.Errorf("the %s ios app's locale %q is not a language and region, like de-DE", name, value)
			}
			a.locale = value
		case "timezone":
			if _, err := time.LoadLocation(value); err != nil || value == "" {
				return nil, fmt.Errorf("the %s ios app's timezone %q is not a time zone, like Europe/Berlin", name, value)
			}
			a.timezone = value
		case "location":
			lat, lon, ok := strings.Cut(value, ",")
			la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
			lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
			if !ok || err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
				return nil, fmt.Errorf("the %s ios app's location %q is not a latitude and a longitude, like 51.3397, 12.3731", name, value)
			}
			a.location = &[2]float64{la, lo}
		case "appium":
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				return nil, fmt.Errorf("the %s ios app's appium %q is not an http(s) URL", name, value)
			}
			a.server = strings.TrimRight(value, "/")
		default:
			return nil, fmt.Errorf("unknown ios app property %q (supported: app, bundle id, device, permissions, locale, timezone, location, appium, capability.<name>)", key)
		}
	}
	switch {
	case a.path == "" && a.bundleID == "":
		return nil, fmt.Errorf("the %s ios app needs its app, or the bundle id of an app the device has", name)
	case a.server == "" && a.device == "":
		return nil, fmt.Errorf("the %s ios app needs a device: a simulator's device type, like iPhone 16, a simulator's name, or its UDID; or an appium server", name)
	case len(a.caps) > 0 && a.server == "":
		return nil, fmt.Errorf("the %s ios app's capability.<name> rows are for its appium server", name)
	case a.server != "" && a.bundleID == "":
		return nil, fmt.Errorf("the %s ios app needs its bundle id with an appium server", name)
	case a.server != "" && len(a.permissions) > 0:
		return nil, fmt.Errorf("the %s ios app's permissions are granted on simulators axx runs: with an appium server, its capabilities grant them", name)
	}
	return a, nil
}

// capabilityValue is a capability as JSON when it is JSON, like an object of
// a device farm's options, and as text otherwise.
func capabilityValue(v string) any {
	var out any
	if strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") || v == "true" || v == "false" {
		if json.Unmarshal([]byte(v), &out) == nil {
			return out
		}
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	return v
}

// launchArguments set the app's language and region, as the Settings app
// would for it alone.
func (a *app) launchArguments() []string {
	m := localeRE.FindStringSubmatch(a.locale)
	lang, locale := m[1], m[1]
	if m[2] != "" {
		lang, locale = m[1]+"-"+m[2], m[1]+"_"+m[2]
	}
	return []string{"-AppleLanguages", "(" + lang + ")", "-AppleLocale", locale}
}
