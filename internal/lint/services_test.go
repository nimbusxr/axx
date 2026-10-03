package lint

import (
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

const servicesFeature = `Feature: services

  Background:
    Given the shop token with the following properties:
      | key        | shop-token-key |
      | claim.shop | maple-crafts   |

  Scenario: a seed before its database
    Given a seeds/scans.json mongo db seed
    And a tracking-db mongo database with the following properties:
      | url | mongodb://localhost/tracking |
    And a seeds/more-scans.json mongo db seed

  Scenario: a request before its service
    Given a GET request to /api/parcels/PX-1
    And the parcels service with the following properties:
      | url | http://localhost:8080 |

  Scenario: services registered first
    Given the parcels service with the following properties:
      | url | http://localhost:8080 |
    And a parcels-db database with the following properties:
      | url | postgres://localhost/parcels |
    And a seeds/parcels.yaml db seed
    And a GET request to /api/parcels/PX-1
`

func TestCheckServices(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("services.feature", []byte(servicesFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pickles {
		for _, ps := range p.Steps {
			if len(e.Registry.Match(ps.Text)) != 1 {
				t.Fatalf("fixture step does not match exactly one definition: %s", ps.Text)
			}
		}
	}
	rr := CheckServices(e.Registry, pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning || f.Code != CodeServiceUnregistered {
			t.Errorf("finding: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s", f.Locations[0].Line, f.Message))
	}
	want := []string{
		"9 this step needs a MongoDB database, but the scenario registers none before it, so it fails: register one first (in the Background for every scenario), like `a tracking-db mongo database with the following properties:`",
		"15 this step needs a REST service, but the scenario registers none before it, so it fails: register one first (in the Background for every scenario), like `the parcels service with the following properties:`",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A project's own pack may register services out of sight: no check.
	e, err = engine.New(engine.Options{Packs: append(engine.Ordered(all.Packs()), engine.NamedPack{Name: "webhooks", Pack: webhookPack{}})})
	if err != nil {
		t.Fatal(err)
	}
	if rr := CheckServices(e.Registry, pickles, ""); len(rr.Findings) != 0 {
		t.Errorf("with a project's pack: %+v", rr.Findings)
	}
}
