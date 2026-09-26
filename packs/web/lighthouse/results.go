package lighthouse

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// category is a Lighthouse category, as the steps name it.
type category struct{ name, id string }

var categories = []category{
	{"performance", "performance"},
	{"accessibility", "accessibility"},
	{"best practices", "best-practices"},
	{"seo", "seo"},
}

// metric is one of the metrics of Lighthouse's navigation audits (the
// performance category's "metrics" group, less interaction to next paint,
// which only a timespan measures), as the steps name it.
type metric struct {
	name, id string
	// acronym is Lighthouse's, which names the metric in what its insights
	// would save.
	acronym string
	// shift is whether it is the layout shift, a number; the others are
	// times, in milliseconds.
	shift bool
}

var metrics = []metric{
	{name: "largest contentful paint", id: "largest-contentful-paint", acronym: "LCP"},
	{name: "first contentful paint", id: "first-contentful-paint", acronym: "FCP"},
	{name: "total blocking time", id: "total-blocking-time", acronym: "TBT"},
	{name: "speed index", id: "speed-index"},
	{name: "cumulative layout shift", id: "cumulative-layout-shift", acronym: "CLS", shift: true},
}

// minimum is the lowest score a category may have.
type minimum struct {
	category
	score int
}

// limit is the most a metric may be: milliseconds, or the layout shift.
type limit struct {
	metric
	most float64
	text string // as the table has it
}

// parseScores reads the table of "the {string} page scores at least:";
// expand expands a value's ${env:..} and ${sys:..} references.
func parseScores(expand func(string) string, t *core.Table) ([]minimum, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("the table lists no category; list them with their lowest score, like | performance | 90 |")
	}
	var out []minimum
	for _, p := range pairs {
		i := slices.IndexFunc(categories, func(c category) bool { return strings.EqualFold(c.name, strings.TrimSpace(p.Key)) })
		if i < 0 {
			return nil, fmt.Errorf("unknown category %q; the categories are %s", p.Key, names(categories, func(c category) string { return c.name }))
		}
		c := categories[i]
		v := strings.TrimSpace(expand(p.Value))
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100 {
			return nil, fmt.Errorf("the lowest %s score %q is not a whole number from 0 to 100", c.name, v)
		}
		if slices.ContainsFunc(out, func(m minimum) bool { return m.id == c.id }) {
			return nil, fmt.Errorf("the table lists %s twice", c.name)
		}
		out = append(out, minimum{c, n})
	}
	return out, nil
}

// parseLimits reads the table of "the {string} page loads within:".
func parseLimits(expand func(string) string, t *core.Table) ([]limit, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("the table lists no metric; list them with their limit, like | largest contentful paint | 2.5s |")
	}
	var out []limit
	for _, p := range pairs {
		i := slices.IndexFunc(metrics, func(m metric) bool { return strings.EqualFold(m.name, strings.TrimSpace(p.Key)) })
		if i < 0 {
			return nil, fmt.Errorf("unknown metric %q; the metrics are %s", p.Key, names(metrics, func(m metric) string { return m.name }))
		}
		m := metrics[i]
		v := strings.TrimSpace(expand(p.Value))
		l := limit{metric: m, text: v}
		if m.shift {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 || math.IsInf(f, 0) {
				return nil, fmt.Errorf("the %s limit %q is not a number like 0.1: a layout shift has no unit", m.name, v)
			}
			l.most = f
		} else {
			d, err := time.ParseDuration(v)
			if err != nil || d < 0 {
				return nil, fmt.Errorf("the %s limit %q is not a duration like 2.5s or 200ms", m.name, v)
			}
			l.most = float64(d) / float64(time.Millisecond)
		}
		if slices.ContainsFunc(out, func(o limit) bool { return o.id == m.id }) {
			return nil, fmt.Errorf("the table lists %s twice", m.name)
		}
		out = append(out, l)
	}
	return out, nil
}

// names lists things as a sentence does: a, b, c and d.
func names[T any](all []T, name func(T) string) string {
	s := make([]string, len(all))
	for i, x := range all {
		s[i] = name(x)
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

// lhr is what the pack reads of Lighthouse's result, its LHR.
type lhr struct {
	LighthouseVersion string   `json:"lighthouseVersion"`
	RequestedURL      string   `json:"requestedUrl"`
	FinalURL          string   `json:"finalDisplayedUrl"`
	RunWarnings       []string `json:"runWarnings"`
	// RuntimeError says why Lighthouse could not audit the page, as when
	// it answers with an error status.
	RuntimeError *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"runtimeError"`
	Categories map[string]lhrCategory `json:"categories"`
	Audits     map[string]lhrAudit    `json:"audits"`
}

type lhrCategory struct {
	// Score is from 0 to 1, or null when an audit that counts failed to run.
	Score     *float64 `json:"score"`
	AuditRefs []struct {
		ID     string  `json:"id"`
		Weight float64 `json:"weight"`
		Group  string  `json:"group"`
	} `json:"auditRefs"`
}

type lhrAudit struct {
	Title string   `json:"title"`
	Score *float64 `json:"score"`
	// Mode is how the score reads: numeric, binary, metricSavings (an
	// insight), informative, manual, notApplicable or error.
	Mode          string             `json:"scoreDisplayMode"`
	NumericValue  *float64           `json:"numericValue"`
	DisplayValue  string             `json:"displayValue"`
	MetricSavings map[string]float64 `json:"metricSavings"`
	ErrorMessage  string             `json:"errorMessage"`
}

// Lighthouse passes an audit that scores 0.9 or more.
const passing = 0.9

// shown is how many audits a failure lists below a category or a metric.
const shown = 5

// checkScores fails with each category that scores below its minimum, and
// the audits that cost it the most.
func checkScores(sc *core.Scenario, what string, r *lhr, mins []minimum) error {
	var missed, met []string
	for _, m := range mins {
		c, ok := r.Categories[m.id]
		if !ok || c.Score == nil {
			missed = append(missed, fmt.Sprintf("  %s: no score, at least %d", m.name, m.score)+lines(r.errored(m.id)))
			continue
		}
		got := int(math.Round(*c.Score * 100))
		if got >= m.score {
			met = append(met, fmt.Sprintf("%s: %d", m.name, got))
			continue
		}
		missed = append(missed, fmt.Sprintf("  %s: %d, at least %d", m.name, got, m.score)+lines(r.failed(m.id)))
	}
	if len(missed) == 0 {
		sc.Log("%s scores as it should (%s)", what, strings.Join(met, ", "))
		return nil
	}
	return failure(what+" scores below what it should:", missed, met)
}

// checkMetrics fails with each metric above its limit, and the insights
// that would bring it down the most.
func checkMetrics(sc *core.Scenario, what string, r *lhr, limits []limit) error {
	var missed, met []string
	for _, l := range limits {
		a, ok := r.Audits[l.id]
		if !ok || a.NumericValue == nil {
			reason := "not measured"
			if a.ErrorMessage != "" {
				reason += " (" + a.ErrorMessage + ")"
			}
			missed = append(missed, fmt.Sprintf("  %s: %s, at most %s", l.name, reason, l.text))
			continue
		}
		got, text := l.measured(*a.NumericValue)
		if got <= l.most {
			met = append(met, l.name+": "+text)
			continue
		}
		missed = append(missed, fmt.Sprintf("  %s: %s, at most %s", l.name, text, l.text)+lines(r.saving(l.acronym)))
	}
	if len(missed) == 0 {
		sc.Log("%s loads within its limits (%s)", what, strings.Join(met, ", "))
		return nil
	}
	return failure(what+" does not load within its limits:", missed, met)
}

// measured is a metric's value as the step shows it, to the millisecond or
// to 4 decimals, and its text.
func (m metric) measured(v float64) (float64, string) {
	if m.shift {
		v = math.Round(v*1e4) / 1e4
		return v, strconv.FormatFloat(v, 'f', -1, 64)
	}
	v = math.Round(v)
	return v, (time.Duration(v) * time.Millisecond).String()
}

func failure(head string, missed, met []string) error {
	msg := head + "\n" + strings.Join(missed, "\n")
	if len(met) > 0 {
		msg += "\n  (" + strings.Join(met, ", ") + ")"
	}
	return core.Failf("%s", msg)
}

// failed are the audits of a category that failed, those that cost the
// most first: those that count towards the score by their weight, then the
// others, the insights, by the time they would save.
func (r *lhr) failed(categoryID string) []lhrAudit {
	type ranked struct {
		lhrAudit
		weight, saves float64
	}
	var out []ranked
	for _, ref := range r.Categories[categoryID].AuditRefs {
		a, ok := r.Audits[ref.ID]
		if !ok || ref.Group == "hidden" || a.Score == nil || *a.Score >= passing {
			continue
		}
		switch a.Mode {
		case "numeric", "binary", "metricSavings":
		default:
			continue
		}
		var saves float64
		for m, v := range a.MetricSavings {
			if m != "CLS" {
				saves += v
			}
		}
		out = append(out, ranked{a, ref.Weight, saves})
	}
	slices.SortStableFunc(out, func(a, b ranked) int {
		return cmp.Or(cmp.Compare(b.weight, a.weight), cmp.Compare(b.saves, a.saves))
	})
	audits := make([]lhrAudit, len(out))
	for i, a := range out {
		audits[i] = a.lhrAudit
	}
	return audits
}

// saving are the failed insights of the performance category that would
// bring a metric down, by Lighthouse's acronym for it, those that would
// save the most first.
func (r *lhr) saving(acronym string) []lhrAudit {
	if acronym == "" {
		return nil
	}
	var out []lhrAudit
	for _, a := range r.failed("performance") {
		if a.MetricSavings[acronym] > 0 {
			out = append(out, a)
		}
	}
	slices.SortStableFunc(out, func(a, b lhrAudit) int {
		return cmp.Compare(b.MetricSavings[acronym], a.MetricSavings[acronym])
	})
	return out
}

// errored are the audits of a category that could not run.
func (r *lhr) errored(categoryID string) []lhrAudit {
	var out []lhrAudit
	for _, ref := range r.Categories[categoryID].AuditRefs {
		if a, ok := r.Audits[ref.ID]; ok && a.Mode == "error" {
			a.DisplayValue = a.ErrorMessage
			out = append(out, a)
		}
	}
	return out
}

// lines are audits as a failure lists them, below what they are about.
func lines(audits []lhrAudit) string {
	var b strings.Builder
	for i, a := range audits {
		if i == shown {
			fmt.Fprintf(&b, "\n    and %d more in Lighthouse's report", len(audits)-shown)
			break
		}
		b.WriteString("\n    " + a.Title)
		if a.DisplayValue != "" {
			b.WriteString(": " + a.DisplayValue)
		}
	}
	// Lighthouse puts no-break spaces between numbers and their units.
	return strings.ReplaceAll(b.String(), "\u00a0", " ")
}
