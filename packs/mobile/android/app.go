package mobileandroid

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// app is an Android app a scenario registered.
type app struct {
	name        string
	apk         string // a file of the project, resolved
	pkg         string
	activity    string
	device      string // an AVD, or a device's serial
	permissions []string
	locale      string // like de-DE
	timezone    string // like Europe/Berlin
	location    *[2]float64
	server      string         // an Appium server of the project's own, or a device farm's
	caps        map[string]any // capability.<name> rows, for that server
}

var (
	localeRE     = regexp.MustCompile(`^([a-z]{2,3})(?:[-_]([A-Z]{2}))?$`)
	permissionRE = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
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
		case "apk":
			path, err := sc.Suite().ResolvePath(value)
			if err != nil {
				return nil, fmt.Errorf("the %s android app's apk %q: %w", name, value, err)
			}
			a.apk = path
		case "package":
			a.pkg = value
		case "activity":
			a.activity = value
		case "device":
			a.device = value
		case "permissions":
			for _, perm := range strings.Split(value, ",") {
				perm = strings.TrimSpace(perm)
				if perm == "" {
					continue
				}
				if !permissionRE.MatchString(perm) {
					return nil, fmt.Errorf("the %s android app's permission %q is not a permission, like POST_NOTIFICATIONS", name, perm)
				}
				if !strings.Contains(perm, ".") {
					perm = "android.permission." + perm
				}
				a.permissions = append(a.permissions, perm)
			}
		case "locale":
			if !localeRE.MatchString(value) {
				return nil, fmt.Errorf("the %s android app's locale %q is not a language and region, like de-DE", name, value)
			}
			a.locale = value
		case "timezone":
			if _, err := time.LoadLocation(value); err != nil || value == "" {
				return nil, fmt.Errorf("the %s android app's timezone %q is not a time zone, like Europe/Berlin", name, value)
			}
			a.timezone = value
		case "location":
			lat, lon, ok := strings.Cut(value, ",")
			la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
			lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
			if !ok || err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
				return nil, fmt.Errorf("the %s android app's location %q is not a latitude and a longitude, like 51.3397, 12.3731", name, value)
			}
			a.location = &[2]float64{la, lo}
		case "appium":
			if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
				return nil, fmt.Errorf("the %s android app's appium %q is not an http(s) URL", name, value)
			}
			a.server = strings.TrimRight(value, "/")
		default:
			return nil, fmt.Errorf("unknown android app property %q (supported: apk, package, activity, device, permissions, locale, timezone, location, appium, capability.<name>)", key)
		}
	}
	switch {
	case a.apk == "" && a.pkg == "":
		return nil, fmt.Errorf("the %s android app needs its apk, or the package of an app the device has", name)
	case a.server == "" && a.device == "":
		return nil, fmt.Errorf("the %s android app needs a device: an emulator device (AVD) axx starts, or a device adb lists; or an appium server", name)
	case len(a.caps) > 0 && a.server == "":
		return nil, fmt.Errorf("the %s android app's capability.<name> rows are for its appium server", name)
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

// language and country are the locale's parts, as Appium sets them.
func (a *app) language() (lang, country string) {
	m := localeRE.FindStringSubmatch(a.locale)
	return m[1], m[2]
}
