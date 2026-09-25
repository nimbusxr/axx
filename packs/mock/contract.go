package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/oaslevel"
)

// The axx WireMock extension (ghcr.io/nimbusxr/axx-wiremock) validates every
// call to a mock against the mocked dependency's OpenAPI contract and records
// its findings on WireMock's request journal, as a sub-event of the served
// request. axx reads them from there: nothing in WireMock is changed, so
// scenarios running in parallel cannot affect each other.
//
// Findings on a call a scenario's mock step checks fail that step (with the
// scenario's levels applied); every other ERROR finding of the run is
// reported once the scenarios have finished (Finish).
const (
	findingsEvent = "openapi-validation"
	// findingsFormat is the version of the findings record this axx reads.
	findingsFormat = 1
)

// finding is one validation finding, as the extension records it.
type finding struct {
	Key     string   `json:"key"`
	Level   string   `json:"level"`
	Side    string   `json:"side"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

// findingsRecord is the extension's record of one validated call.
type findingsRecord struct {
	Format    int       `json:"format"`
	Extension string    `json:"extension"`
	Spec      string    `json:"spec"`
	Mode      string    `json:"mode"`
	Findings  []finding `json:"findings"`
}

// served is one journaled request with the extension's record, if any.
type served struct {
	ID     string
	Method string
	URL    string
	Record *findingsRecord
}

// served returns the requests WireMock received since t, oldest first.
func (c *client) served(ctx context.Context, since time.Time) ([]served, error) {
	var out struct {
		Requests []struct {
			ID      string `json:"id"`
			Request struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			} `json:"request"`
			SubEvents []struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			} `json:"subEvents"`
		} `json:"requests"`
	}
	q := url.Values{"since": {since.UTC().Format("2006-01-02T15:04:05.000Z")}}
	if err := c.get(ctx, "/__admin/requests?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	reqs := make([]served, 0, len(out.Requests))
	for i := len(out.Requests) - 1; i >= 0; i-- { // the journal lists newest first
		r := out.Requests[i]
		s := served{ID: r.ID, Method: r.Request.Method, URL: r.Request.URL}
		for _, e := range r.SubEvents {
			if e.Type != findingsEvent {
				continue
			}
			var rec findingsRecord
			if err := json.Unmarshal(e.Data, &rec); err != nil {
				return nil, fmt.Errorf("cannot read the OpenAPI findings WireMock at %s recorded: %w", c.base, err)
			}
			if rec.Format != findingsFormat {
				return nil, fmt.Errorf("WireMock at %s records OpenAPI findings in format %d (axx-wiremock %s), and this axx reads format %d; "+
					"use an axx-wiremock image that matches this axx version (see the extension's README)", c.base, rec.Format, rec.Extension, findingsFormat)
			}
			s.Record = &rec
		}
		reqs = append(reqs, s)
	}
	return reqs, nil
}

// contracts is the run-wide state: when the run started, the mocked services
// used, the requests some scenario already answered for, and the relaxations
// scenarios set.
type contracts struct {
	start time.Time

	mu       sync.Mutex
	services map[string]*mockedURL // by base URL
	checked  map[string]bool       // served request IDs a scenario checked
	relaxed  map[string][]relaxation
}

// relaxation is a key a scenario relaxed on a mocked service.
type relaxation struct {
	key      string
	level    oaslevel.Level
	scenario string // "name" (uri:line)
}

type mockedURL struct {
	name string
	c    *client
}

const contractsKey = "mock/contracts"

func runContracts(s *core.Suite) *contracts {
	c, _ := core.Cached(s, contractsKey, func() (*contracts, error) {
		return &contracts{
			start: time.Now(), services: map[string]*mockedURL{}, checked: map[string]bool{},
			relaxed: map[string][]relaxation{},
		}, nil
	})
	return c
}

func (k *contracts) use(svc *Service) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.services[svc.c.base]; !ok {
		k.services[svc.c.base] = &mockedURL{name: svc.Name, c: svc.c}
	}
}

func (k *contracts) relax(svc *Service, lv oaslevel.Levels, info core.ScenarioInfo) {
	where := fmt.Sprintf("%q (%s:%d)", info.Name, info.URI, info.Line)
	keys := make([]string, 0, len(lv))
	for key := range lv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		k.relaxed[svc.c.base] = append(k.relaxed[svc.c.base], relaxation{key: key, level: lv[key], scenario: where})
	}
}

// relaxedBy returns the scenarios that relaxed a key covering findingKey on
// the mocked service at base.
func (k *contracts) relaxedBy(base, findingKey string) []relaxation {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []relaxation
	for _, r := range k.relaxed[base] {
		if r.level != oaslevel.Error && (findingKey == r.key || strings.HasPrefix(findingKey, r.key+".")) {
			out = append(out, r)
		}
	}
	return out
}

func (k *contracts) markChecked(ids []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, id := range ids {
		k.checked[id] = true
	}
}

// checkContract applies the scenario's levels to the findings on the calls
// matching p (method and exact URL) and fails on any ERROR.
func checkContract(sc *core.Scenario, svc *Service, name string, p *pattern) error {
	k := runContracts(sc.Suite())
	reqs, err := svc.c.served(sc.Context(), k.start)
	if err != nil {
		return err
	}
	var ids, problems []string
	spec := ""
	for _, r := range reqs {
		if r.Record == nil || r.Method != p.Method || r.URL != p.URL {
			continue
		}
		ids = append(ids, r.ID)
		svc.checked++
		spec = r.Record.Spec
		for _, f := range r.Record.Findings {
			switch svc.level(f) {
			case oaslevel.Error:
				problems = append(problems, f.line())
			case oaslevel.Warn:
				sc.Log("warning: %s %s (%s): %s", p.Method, p.URL, f.Key, f.Message)
			}
		}
	}
	k.markChecked(ids)
	if len(problems) == 0 {
		return nil
	}
	msg := fmt.Sprintf("The %s %s request named %s broke the contract of the mocked %s service (%s):\n%s\n"+
		"To relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "+
		"\"Given the OpenAPI validation levels for the mocked %s service are:\".",
		p.Method, p.URL, name, svc.Name, spec, strings.Join(problems, "\n"), svc.Name)
	return core.Failf("%s", msg)
}

// level is a finding's level in the scenario: the scenario's own levels for
// the service when one covers the key, else the level the extension recorded
// (from the stub and the extension's defaults).
func (s *Service) level(f finding) oaslevel.Level {
	if lv, ok := s.levels.Find(f.Key); ok {
		return lv
	}
	lv, err := oaslevel.Parse(f.Level)
	if err != nil {
		return oaslevel.Error
	}
	return lv
}

func (f finding) line() string {
	s := "  - " + f.Key + " (" + f.Side + "): " + f.Message
	for _, d := range f.Details {
		s += "\n      " + d
	}
	return s
}

// finish reports the ERROR findings of the run on calls no scenario checked.
func (k *contracts) finish(ctx context.Context) error {
	k.mu.Lock()
	bases := make([]string, 0, len(k.services))
	for b := range k.services {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	k.mu.Unlock()

	var blocks, failures []string
	for _, base := range bases {
		k.mu.Lock()
		m := k.services[base]
		k.mu.Unlock()
		reqs, err := m.c.served(ctx, k.start)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		var lines []string
		for _, r := range reqs {
			k.mu.Lock()
			done := k.checked[r.ID]
			k.mu.Unlock()
			if r.Record == nil || done {
				continue
			}
			var errs []string
			for _, f := range r.Record.Findings {
				if lv, err := oaslevel.Parse(f.Level); err != nil || lv == oaslevel.Error {
					errs = append(errs, "  "+f.line())
					for _, rx := range k.relaxedBy(base, f.Key) {
						errs = append(errs, fmt.Sprintf("        relaxed to %s in %s, which does not check this call: "+
							"check it there with a mock step, or relax it on the stub", rx.level, rx.scenario))
					}
				}
			}
			if len(errs) > 0 {
				lines = append(lines, "  "+r.Method+" "+r.URL+" ("+r.Record.Spec+")\n"+strings.Join(errs, "\n"))
			}
		}
		if len(lines) > 0 {
			blocks = append(blocks, fmt.Sprintf("Calls to the mocked %s service (%s) broke its contract, and no scenario checked them:\n%s",
				m.name, base, strings.Join(lines, "\n")))
		}
	}
	if len(blocks) > 0 {
		blocks = append(blocks, "Check the call in the scenario that makes it (a mock step names it), or relax the check with the "+
			"extension's levels (stub metadata openApiValidationLevels, or OPENAPI_VALIDATION_LEVELS).")
	}
	blocks = append(blocks, failures...)
	if len(blocks) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(blocks, "\n\n"))
}
