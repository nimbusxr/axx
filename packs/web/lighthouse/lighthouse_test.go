package lighthouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestTheDeviceIsAPhoneUnlessSaidOtherwise(t *testing.T) {
	for device, want := range map[string]string{"": "mobile", "mobile": "mobile", "desktop": "desktop"} {
		if st, err := parseConfig(Config{Device: device}); err != nil || st.device != want {
			t.Errorf("%q: %v %v, want %s", device, st, err, want)
		}
	}
	if _, err := parseConfig(Config{Device: "tablet"}); err == nil || err.Error() != `packs.web-lighthouse.device: "tablet" is not one of mobile, desktop` {
		t.Errorf("tablet: %v", err)
	}
}

func table(rows ...[]string) *core.Table { return &core.Table{Rows: rows} }

var noExpand = func(s string) string { return s }

func TestScoresAreReadFromTheTable(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "${env:SEO_SCORE}", "85") }
	mins, err := parseScores(expand, table([]string{"performance", "90"}, []string{"Best Practices", " 95 "}, []string{"SEO", "${env:SEO_SCORE}"}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range mins {
		got = append(got, fmt.Sprintf("%s=%d", m.id, m.score))
	}
	if strings.Join(got, " ") != "performance=90 best-practices=95 seo=85" {
		t.Errorf("minimums: %v", got)
	}
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{nil, "the table lists no category; list them with their lowest score, like | performance | 90 |"},
		{[][]string{{"speed", "90"}}, `unknown category "speed"; the categories are performance, accessibility, best practices and seo`},
		{[][]string{{"performance", "0.9"}}, `the lowest performance score "0.9" is not a whole number from 0 to 100`},
		{[][]string{{"accessibility", "101"}}, `the lowest accessibility score "101" is not a whole number from 0 to 100`},
		{[][]string{{"seo", "90"}, {"SEO", "80"}}, "the table lists seo twice"},
		{[][]string{{"seo"}}, "data table row 1 has 1 cells; expected 2 (key | value)"},
	} {
		if _, err := parseScores(noExpand, table(c.rows...)); err == nil || err.Error() != c.want {
			t.Errorf("%v: %v, want %s", c.rows, err, c.want)
		}
	}
}

func TestLimitsAreDurationsExceptTheShift(t *testing.T) {
	limits, err := parseLimits(noExpand, table(
		[]string{"largest contentful paint", "2.5s"}, []string{"Total Blocking Time", "200ms"}, []string{"cumulative layout shift", "0.1"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 3 || limits[0].most != 2500 || limits[1].most != 200 || limits[2].most != 0.1 || limits[0].text != "2.5s" {
		t.Errorf("limits: %+v", limits)
	}
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{nil, "the table lists no metric; list them with their limit, like | largest contentful paint | 2.5s |"},
		{[][]string{{"time to interactive", "3s"}}, `unknown metric "time to interactive"; the metrics are largest contentful paint, first contentful paint, total blocking time, speed index and cumulative layout shift`},
		{[][]string{{"speed index", "3.4"}}, `the speed index limit "3.4" is not a duration like 2.5s or 200ms`},
		{[][]string{{"first contentful paint", "-1s"}}, `the first contentful paint limit "-1s" is not a duration like 2.5s or 200ms`},
		{[][]string{{"cumulative layout shift", "0.1s"}}, `the cumulative layout shift limit "0.1s" is not a number like 0.1: a layout shift has no unit`},
		{[][]string{{"speed index", "3s"}, {"Speed Index", "4s"}}, "the table lists speed index twice"},
	} {
		if _, err := parseLimits(noExpand, table(c.rows...)); err == nil || err.Error() != c.want {
			t.Errorf("%v: %v, want %s", c.rows, err, c.want)
		}
	}
}

// recorded is Lighthouse 13.5.0's result of a mobile audit of a page with
// problems, trimmed to what the pack reads.
func recorded(t *testing.T) *lhr {
	t.Helper()
	b, err := os.ReadFile("testdata/lhr-quote.json")
	if err != nil {
		t.Fatal(err)
	}
	var r lhr
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func scenario() *core.Scenario {
	return core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
}

func TestScoresBelowTheirMinimumSayWhatCostThePageTheMost(t *testing.T) {
	mins, err := parseScores(noExpand, table([]string{"performance", "90"}, []string{"accessibility", "100"}, []string{"best practices", "95"}, []string{"seo", "80"}))
	if err != nil {
		t.Fatal(err)
	}
	err = checkScores(scenario(), `The "/quote" page`, recorded(t), mins)
	want := "The \"/quote\" page scores below what it should:\n" +
		"  performance: 78, at least 90\n" +
		"    Total Blocking Time: 940 ms\n" +
		"    Use efficient cache lifetimes: Est savings of 78 KiB\n" +
		"    Improve image delivery: Est savings of 80 KiB\n" +
		"    LCP request discovery\n" +
		"    Image elements do not have explicit `width` and `height`\n" +
		"    and 1 more in Lighthouse's report\n" +
		"  accessibility: 81, at least 100\n" +
		"    Image elements do not have `[alt]` attributes\n" +
		"    Background and foreground colors do not have a sufficient contrast ratio.\n" +
		"  (best practices: 96, seo: 83)"
	if !core.IsAssertion(err) || err.Error() != want {
		t.Errorf("got:\n%v\nwant:\n%s", err, want)
	}
	if err := checkScores(scenario(), `The "/quote" page`, recorded(t), mins[1:2:2]); err == nil {
		t.Error("an accessibility score of 81 passes 100")
	}
	mins[1].score = 81
	if err := checkScores(scenario(), `The "/quote" page`, recorded(t), mins[1:2:2]); err != nil {
		t.Errorf("an accessibility score of 81 fails 81: %v", err)
	}
}

func TestMetricsAboveTheirLimitSayWhatWouldBringThemDown(t *testing.T) {
	limits, err := parseLimits(noExpand, table(
		[]string{"largest contentful paint", "1s"}, []string{"first contentful paint", "1.8s"}, []string{"total blocking time", "200ms"},
		[]string{"speed index", "3.4s"}, []string{"cumulative layout shift", "0.1"},
	))
	if err != nil {
		t.Fatal(err)
	}
	err = checkMetrics(scenario(), `The "/quote" page`, recorded(t), limits)
	want := "The \"/quote\" page does not load within its limits:\n" +
		"  largest contentful paint: 1.351s, at most 1s\n" +
		"    Use efficient cache lifetimes: Est savings of 78 KiB\n" +
		"    Improve image delivery: Est savings of 80 KiB\n" +
		"  total blocking time: 938ms, at most 200ms\n" +
		"  (first contentful paint: 605ms, speed index: 779ms, cumulative layout shift: 0.0745)"
	if !core.IsAssertion(err) || err.Error() != want {
		t.Errorf("got:\n%v\nwant:\n%s", err, want)
	}
	// A metric is compared as the step shows it: 938ms is within 938ms.
	limits, _ = parseLimits(noExpand, table([]string{"total blocking time", "938ms"}, []string{"cumulative layout shift", "0.0745"}))
	if err := checkMetrics(scenario(), `The "/quote" page`, recorded(t), limits); err != nil {
		t.Errorf("at the limits: %v", err)
	}
}

func TestAScoreLighthouseCouldNotGiveSaysWhy(t *testing.T) {
	r := recorded(t)
	c := r.Categories["performance"]
	c.Score = nil
	r.Categories["performance"] = c
	lcp := r.Audits["largest-contentful-paint"]
	lcp.Mode, lcp.Score, lcp.NumericValue, lcp.ErrorMessage = "error", nil, nil, "The page did not display content that qualifies as a Largest Contentful Paint (LCP). (NO_LCP)"
	r.Audits["largest-contentful-paint"] = lcp
	err := checkScores(scenario(), `The "/quote" page`, r, []minimum{{categories[0], 50}})
	want := "The \"/quote\" page scores below what it should:\n" +
		"  performance: no score, at least 50\n" +
		"    Largest Contentful Paint: The page did not display content that qualifies as a Largest Contentful Paint (LCP). (NO_LCP)"
	if err == nil || err.Error() != want {
		t.Errorf("got:\n%v\nwant:\n%s", err, want)
	}
	err = checkMetrics(scenario(), `The "/quote" page`, r, []limit{{metric: metrics[0], most: 2500, text: "2.5s"}})
	want = "The \"/quote\" page does not load within its limits:\n" +
		"  largest contentful paint: not measured (The page did not display content that qualifies as a Largest Contentful Paint (LCP). (NO_LCP)), at most 2.5s"
	if err == nil || err.Error() != want {
		t.Errorf("got:\n%v\nwant:\n%s", err, want)
	}
}

func TestPagesAreBelowTheWebAppsURL(t *testing.T) {
	const base = "http://localhost:8080/shop"
	for page, want := range map[string]string{
		"/quote":                         base + "/quote",
		"track/PX-4101":                  base + "/track/PX-4101",
		"https://parcels.example.com/":   "https://parcels.example.com/",
		"http://localhost:8080/about-us": "http://localhost:8080/about-us",
	} {
		if got := pageURL(base, page); got != want {
			t.Errorf("%s: %s, want %s", page, got, want)
		}
	}
	for address, want := range map[string]string{
		base + "/login?next=/account": "/login?next=/account",
		base:                          "/",
		"http://localhost:8080/login": "http://localhost:8080/login",
		base + "shop/login":           base + "shop/login",
	} {
		if got := pathOf(base, address); got != want {
			t.Errorf("%s: %s, want %s", address, got, want)
		}
	}
	if trimURL("http://localhost:8080/") != trimURL("http://localhost:8080") || trimURL("http://localhost:8080/quote#price") != "http://localhost:8080/quote" {
		t.Error("trimURL")
	}
}

func TestNodeSaysWhyItFailed(t *testing.T) {
	stderr := "node:internal/modules/esm/resolve:873\n  throw new ERR_MODULE_NOT_FOUND(packageName, fileURLToPath(base), null);\n        ^\n\n" +
		"Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'lighthouse' imported from /cache/axx-lighthouse.mjs\n    at packageResolve (node:internal/modules/esm/resolve:873:9)\n"
	if got := nodeError(stderr, errors.New("exit status 1")); got != "Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'lighthouse' imported from /cache/axx-lighthouse.mjs" {
		t.Errorf("a missing package: %s", got)
	}
	if got := nodeError("Killed\n", errors.New("signal: killed")); got != "Killed" {
		t.Errorf("no error line: %s", got)
	}
	if got := nodeError("", errors.New("signal: killed")); got != "signal: killed" {
		t.Errorf("nothing printed: %s", got)
	}
}
