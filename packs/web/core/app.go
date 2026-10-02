package webcore

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// App is a registered web app: where the browser finds it, the browser that
// uses it and how it presents itself, and the session it starts with.
type App struct {
	Name   string
	URL    string // without a trailing slash
	Engine string // chromium, firefox, webkit, chrome or msedge
	// Device is one of Playwright's device descriptors, like "iPhone 15".
	Device string
	// Viewport is the size of the page, in CSS pixels, like "1280x720".
	Viewport      *Size
	Locale        string // like de-DE
	Timezone      string // like Europe/Berlin
	ColorScheme   string // light, dark or no-preference
	ReducedMotion string // reduce or no-preference
	Media         string // screen or print
	UserAgent     string
	Location      *Location
	Permissions   []string
	// InsecureTLS accepts any certificate of the app (tls.verify false).
	InsecureTLS bool

	// The session the browser starts with, for the app's address only.
	Cookies        []Setting
	Headers        []Setting
	LocalStorage   []Setting
	SessionStorage []Setting
	Username       string
	Password       string
}

// Size is a width and a height.
type Size struct{ Width, Height int }

// Location is where the browser says it is.
type Location struct{ Latitude, Longitude float64 }

// Setting is a name and a value: a cookie, a header, a storage item.
type Setting struct{ Name, Value string }

// key identifies the browser an app uses, shared by the scenarios of a run.
func (a *App) key() string { return a.Engine }

// origin is the app's scheme, host and port.
func (a *App) origin() string {
	u, err := url.Parse(a.URL)
	if err != nil {
		return a.URL
	}
	return u.Scheme + "://" + u.Host
}

var (
	properties = []string{
		"url", "engine", "device", "viewport", "locale", "timezone", "color scheme", "reduced motion", "media",
		"user agent", "location", "permissions", "tls.verify", "username", "password",
		"cookie.<name>", "header.<name>", "local storage.<key>", "session storage.<key>",
	}
	engines      = []string{"chromium", "firefox", "webkit", "chrome", "msedge"}
	sizeText     = regexp.MustCompile(`^(\d+)\s*x\s*(\d+)$`)
	colorSchemes = []string{"light", "dark", "no-preference"}
	motions      = []string{"reduce", "no-preference"}
	medias       = []string{"screen", "print"}
)

// parseApp reads a web app's properties; expand expands a value's
// ${env:..} and ${sys:..} references.
func parseApp(expand func(string) string, name string, t *core.Table) (*App, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	a := &App{Name: name, Engine: "chromium"}
	oneOf := func(key, v string, allowed []string) (string, error) {
		if !slices.Contains(allowed, v) {
			return "", fmt.Errorf("the %s web app's %s %q is not one of %s", name, key, v, strings.Join(allowed, ", "))
		}
		return v, nil
	}
	for _, p := range pairs {
		v := strings.TrimSpace(expand(p.Value))
		key := p.Key
		if prefix, rest, ok := strings.Cut(key, "."); ok && rest != "" {
			item := Setting{Name: rest, Value: v}
			switch prefix {
			case "cookie":
				a.Cookies = append(a.Cookies, item)
				continue
			case "header":
				a.Headers = append(a.Headers, item)
				continue
			case "local storage":
				a.LocalStorage = append(a.LocalStorage, item)
				continue
			case "session storage":
				a.SessionStorage = append(a.SessionStorage, item)
				continue
			}
		}
		switch key {
		case "url":
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, fmt.Errorf("the %s web app's url %q is not an http(s) URL", name, v)
			}
			a.URL = strings.TrimRight(v, "/")
		case "engine":
			if a.Engine, err = oneOf(key, v, engines); err != nil {
				return nil, err
			}
		case "device":
			a.Device = v
		case "viewport":
			m := sizeText.FindStringSubmatch(v)
			if m == nil {
				return nil, fmt.Errorf("the %s web app's viewport %q is not a size like 1280x720", name, v)
			}
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			if w == 0 || h == 0 {
				return nil, fmt.Errorf("the %s web app's viewport %q is empty", name, v)
			}
			a.Viewport = &Size{w, h}
		case "locale":
			a.Locale = v
		case "timezone":
			a.Timezone = v
		case "color scheme":
			if a.ColorScheme, err = oneOf(key, v, colorSchemes); err != nil {
				return nil, err
			}
		case "reduced motion":
			if a.ReducedMotion, err = oneOf(key, v, motions); err != nil {
				return nil, err
			}
		case "media":
			if a.Media, err = oneOf(key, v, medias); err != nil {
				return nil, err
			}
		case "user agent":
			a.UserAgent = v
		case "location":
			lat, lon, ok := strings.Cut(v, ",")
			la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
			lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
			if !ok || err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
				return nil, fmt.Errorf("the %s web app's location %q is not a latitude and a longitude, like 52.5200, 13.4050", name, v)
			}
			a.Location = &Location{la, lo}
		case "permissions":
			for _, perm := range strings.Split(v, ",") {
				if perm = strings.TrimSpace(perm); perm != "" {
					a.Permissions = append(a.Permissions, perm)
				}
			}
		case "tls.verify":
			verify, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("the %s web app's tls.verify %q is not true or false", name, v)
			}
			a.InsecureTLS = !verify
		case "username":
			a.Username = v
		case "password":
			a.Password = v
		default:
			return nil, fmt.Errorf("unknown web app property %q (supported: %s)", p.Key, strings.Join(properties, ", "))
		}
	}
	if a.URL == "" {
		return nil, fmt.Errorf("the web app property %q is required", "url")
	}
	if (a.Username == "") != (a.Password == "") {
		return nil, fmt.Errorf("the %s web app needs both a username and a password, for HTTP authentication", name)
	}
	return a, nil
}

// pageURL is the address of a page of the app: a path below the app's URL,
// or an absolute URL.
func (a *App) pageURL(page string) string {
	if strings.HasPrefix(page, "http://") || strings.HasPrefix(page, "https://") {
		return page
	}
	return a.URL + "/" + strings.TrimLeft(page, "/")
}

// pagePath is where a browser address is within the app: the path below the
// app's URL (and the query, when want asks for one), or the whole address
// when it is not the app's.
func (a *App) pagePath(address, want string) string {
	rest, ok := strings.CutPrefix(address, a.URL)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "?")) {
		return address
	}
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest = rest[:i]
	}
	if !strings.Contains(want, "?") {
		rest, _, _ = strings.Cut(rest, "?")
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return rest
}

var apps = core.NewStateKey(Name+"/apps", func(*core.Scenario) *core.Services[*App] {
	return core.NewServices[*App]("web app",
		`No web app is registered in this scenario; register one with "the {word} web app with the following properties:"`).RegisteredBy("the {word} web app with the following properties:")
}, nil)
