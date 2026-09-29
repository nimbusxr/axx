package mobileios

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

func TestRegistrationErrors(t *testing.T) {
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	h.File("Courier.app/Info.plist", "")
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"device", "iPhone 16"}}, "needs its app, or the bundle id of an app the device has"},
		{[][]string{{"bundle id", "example.parcels.courier"}}, "needs a device"},
		{[][]string{{"app", "Courier.ipa"}, {"device", "iPhone 16"}}, "is for devices: simulators run a simulator build"},
		{[][]string{{"bundle id", "courier"}, {"device", "iPhone 16"}}, "is not a bundle identifier"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"locale", "German"}}, "is not a language and region"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"timezone", "Berlin/Mitte"}}, "is not a time zone"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"location", "Leipzig"}}, "is not a latitude and a longitude"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"permissions", "notifications"}}, "iOS simulators cannot grant notifications"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"permissions", "all"}}, "name each service"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"permissions", "Photo Library"}}, "is not a service the simulator grants"},
		{[][]string{{"bundle id", "a.b"}, {"appium", "farm.example"}}, "is not an http(s) URL"},
		{[][]string{{"app", "Courier.app"}, {"appium", "https://farm.example"}}, "needs its bundle id with an appium server"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"capability.bstack:options", "{}"}}, "are for its appium server"},
		{[][]string{{"bundle id", "a.b"}, {"appium", "https://farm.example"}, {"permissions", "location"}}, "its capabilities grant them"},
		{[][]string{{"bundle id", "a.b"}, {"device", "d"}, {"screen", "big"}}, `unknown ios app property "screen"`},
	} {
		_ = h.Fails("the courier ios app with the following properties:", c.want, c.rows)
	}
}

// A simulator's session: WebDriverAgent as Appium builds it, on ports of
// the simulator's own; axx reset the app, so Appium leaves it.
func TestCapabilities(t *testing.T) {
	a := &app{locale: "en-US", caps: map[string]any{}}
	caps := a.capabilities(&device{udid: "U", version: "18.1", wdaPort: 8101, mjpegPort: 9101}, "example.parcels.courier")
	for key, want := range map[string]any{
		"platformName": "iOS", "appium:automationName": "XCUITest", "appium:bundleId": "example.parcels.courier",
		"appium:udid": "U", "appium:platformVersion": "18.1", "appium:usePreinstalledWDA": true,
		"appium:wdaLocalPort": 8101, "appium:mjpegServerPort": 9101, "appium:noReset": true, "appium:autoLaunch": false,
	} {
		if caps[key] != want {
			t.Errorf("capability %s: %v, want %v", key, caps[key], want)
		}
	}
	farm := &app{locale: "en-US", caps: map[string]any{"bstack:options": capabilityValue(`{"deviceName": "iPhone 16"}`)}}
	caps = farm.capabilities(nil, "example.parcels.courier")
	b, _ := json.Marshal(caps["bstack:options"])
	if string(b) != `{"deviceName":"iPhone 16"}` || caps["appium:usePreinstalledWDA"] != nil || caps["appium:noReset"] != false {
		t.Errorf("a farm's caps: %v", caps)
	}
}

// The app starts in the registration's language and region.
func TestLaunchArguments(t *testing.T) {
	for locale, want := range map[string]string{
		"de-DE": "-AppleLanguages (de-DE) -AppleLocale de_DE",
		"en":    "-AppleLanguages (en) -AppleLocale en",
	} {
		a := &app{locale: locale}
		if got := strings.Join(a.launchArguments(), " "); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
}

func TestConfig(t *testing.T) {
	s, err := parseConfig(Config{Devices: 2, BootTimeout: "90s"})
	if err != nil || s.devices != 2 || s.bootTimeout.Seconds() != 90 {
		t.Errorf("settings %+v, %v", s, err)
	}
	if _, err := parseConfig(Config{BootTimeout: "soon"}); err == nil || !strings.Contains(err.Error(), "not a duration") {
		t.Errorf("err %v", err)
	}
	if s, _ := parseConfig(Config{}); s.devices != 1 || s.bootTimeout.Minutes() != 20 {
		t.Errorf("one device and 20 minutes by default: %+v", s)
	}
}

// A registration's device: a device type with an iOS version or not, the
// newest runtime by default, and a runtime's version.
func TestDevices(t *testing.T) {
	for device, want := range map[string][2]string{
		"iPhone 16":              {"iPhone 16", ""},
		"iPhone 16, iOS 18.1":    {"iPhone 16", "iOS 18.1"},
		"iPhone 16 ,  iOS  17.5": {"iPhone 16", "iOS 17.5"},
		"parcels-iphone":         {"parcels-iphone", ""},
	} {
		if name, ios := splitDevice(device); name != want[0] || ios != want[1] {
			t.Errorf("%q: %q %q, want %q", device, name, ios, want)
		}
	}
	rts := []simRuntime{{Identifier: "r17", Name: "iOS 17.5", Version: "17.5"}, {Identifier: "r18", Name: "iOS 18.1", Version: "18.1"}, {Identifier: "r9", Name: "iOS 18.0", Version: "18.0"}}
	if r, _ := pickRuntime(rts, ""); r.Identifier != "r18" {
		t.Errorf("the newest: %s", r.Identifier)
	}
	if r, _ := pickRuntime(rts, "iOS 17.5"); r.Identifier != "r17" {
		t.Errorf("the one named: %s", r.Identifier)
	}
	if _, ok := pickRuntime(rts, "iOS 16.4"); ok {
		t.Error("a runtime Xcode lacks")
	}
	if !newer("18.10", "18.9") || newer("18", "18.0") || !newer("26.0", "18.1") {
		t.Error("versions compare by their numbers")
	}
	if v := versionOf("com.apple.CoreSimulator.SimRuntime.iOS-18-1"); v != "18.1" {
		t.Errorf("version %q", v)
	}
	for _, s := range []string{"737939DC-9FBF-4AB5-9795-ED9A5D998D6B", "00008120-001A2B3C4D5E6F70"} {
		if !isUDID(s) {
			t.Errorf("%s is a UDID", s)
		}
	}
	if isUDID("iPhone 16") {
		t.Error("a device type is not a UDID")
	}
	sims := []simDevice{{Name: "iPhone 16", UDID: "737939DC-9FBF-4AB5-9795-ED9A5D998D6B"}}
	if d, ok := findUDID(sims, strings.ToLower(sims[0].UDID)); !ok || d.Name != "iPhone 16" {
		t.Errorf("found %v %v", d, ok)
	}
}

// A set's simulators are its own: simctl is told which set.
func TestSimulatorSets(t *testing.T) {
	if got := strings.Join(simSet("").args("boot", "U"), " "); got != "boot U" {
		t.Errorf("Xcode's set: %q", got)
	}
	if got := strings.Join(simSet("/cache/simulators").args("boot", "U"), " "); got != "--set /cache/simulators boot U" {
		t.Errorf("axx's set: %q", got)
	}
}

// Simulators axx made are named for the axx that made them, so that one a
// killed run left is known as such.
func TestMadeNames(t *testing.T) {
	m := madeRE.FindStringSubmatch("axx-4242-1 iPhone 16-iOS 18.1")
	if m == nil || m[1] != "4242" {
		t.Errorf("match %v", m)
	}
	if slices.ContainsFunc([]string{"iPhone 16", "axx iPhone", "parcels-iphone"}, madeRE.MatchString) {
		t.Error("only axx's own names match")
	}
}
