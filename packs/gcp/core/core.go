// Package gcpcore is the gcp-core pack: the Google Cloud project the gcp-*
// packs talk to, set up as the Google Cloud client libraries are set up for
// the real services, and the client options those packs build their
// clients with.
package gcpcore

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name; the gcp-* packs require it.
const Name = "gcp-core"

const packDoc = `The Google Cloud project the gcp-* packs talk to, set up the way the Google Cloud client libraries are set up for the real services.

Register the project once with ` + "`the {word} gcp project with the following properties:`" + `; every gcp-* step of the scenario uses it (the first project registered is the default).

| Property | |
| --- | --- |
| ` + "`project`" + ` | required: the project ID |
| ` + "`endpoint`" + ` | where every service of the project is, instead of Google Cloud: a local emulator such as ` + "`http://localhost:4588`" + `. The clients then connect without credentials (and without TLS for an ` + "`http://`" + ` endpoint) |
| ` + "`credentials`" + ` | a service account key file (resolved against ` + "`resources`" + `) |

Without ` + "`credentials`" + `, the clients use Application Default Credentials, as they always do: ` + "`GOOGLE_APPLICATION_CREDENTIALS`" + `, the gcloud login, or the workload's service account. Values expand ` + "`${env:..}`" + ` and ` + "`${sys:..}`" + `, so the same features run against Google Cloud and against an emulator.`

// Pack returns the gcp-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: Name,
		Doc:       packDoc,
		Steps: []core.StepDef{{
			ID: "gcp-core.project", Keyword: "Given", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "the {word} gcp project with the following properties:",
			Doc: "Register the Google Cloud project the gcp-* steps talk to: `project` (required), `endpoint` (an emulator), " +
				"`credentials` (a service account key file). Without credentials the clients use Application Default Credentials.",
			Examples:   []string{"Given the billing gcp project with the following properties:"},
			TableTypes: map[string]string{"credentials": "filepath"},
			Run: func(sc *core.Scenario, a core.Args) error {
				p, err := Parse(sc.Suite(), a.String(0), a.Table)
				if err != nil {
					return err
				}
				return projects.Of(sc).Add(p.Name, p)
			},
		}},
	}
}

// Project is a registered Google Cloud project.
type Project struct {
	Name     string
	ID       string
	Endpoint string
	// Credentials is the resolved service account key file.
	Credentials string
}

// Key identifies the project's configuration, for clients and listeners
// shared by the scenarios of a run.
func (p *Project) Key() string { return strings.Join([]string{p.ID, p.Endpoint, p.Credentials}, "|") }

// REST are the client options of a service reached over HTTP; path is the
// service's root on an emulator ("/storage/v1/", "/bigquery/v2/").
func (p *Project) REST(path string) []option.ClientOption {
	if p.Endpoint != "" {
		return []option.ClientOption{option.WithEndpoint(p.Endpoint + path), option.WithoutAuthentication()}
	}
	return p.auth()
}

// GRPC are the client options of a service reached over gRPC.
func (p *Project) GRPC() []option.ClientOption {
	if p.Endpoint == "" {
		return p.auth()
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Host == "" {
		return []option.ClientOption{option.WithEndpoint(p.Endpoint), option.WithoutAuthentication()}
	}
	opts := []option.ClientOption{option.WithEndpoint(u.Host), option.WithoutAuthentication()}
	if u.Scheme == "http" {
		opts = append(opts, option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}
	return opts
}

func (p *Project) auth() []option.ClientOption {
	if p.Credentials != "" {
		return []option.ClientOption{option.WithAuthCredentialsFile(option.ServiceAccount, p.Credentials)}
	}
	return nil
}

var properties = []string{"project", "endpoint", "credentials"}

// Parse reads a project's properties (expanding ${env:..} and ${sys:..}).
// The gcp-* packs also use it to read the projects of a planned run.
func Parse(s *core.Suite, name string, t *core.Table) (*Project, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	p := &Project{Name: name}
	for _, pr := range pairs {
		v := strings.TrimSpace(s.Interpolate(pr.Value))
		switch pr.Key {
		case "project":
			p.ID = v
		case "endpoint":
			p.Endpoint = strings.TrimRight(v, "/")
		case "credentials":
			if v == "" {
				continue
			}
			path, err := s.ResolvePath(v)
			if err != nil {
				return nil, fmt.Errorf("gcp project credentials: %w", err)
			}
			p.Credentials = path
		default:
			return nil, fmt.Errorf("unknown gcp project property %q (supported: %s)", pr.Key, strings.Join(properties, ", "))
		}
	}
	if p.ID == "" {
		return nil, fmt.Errorf(`the gcp project property "project" is required`)
	}
	return p, nil
}

var projects = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*Project] {
	return core.NewServices[*Project]("GCP project",
		`No GCP project is registered in this scenario; register one with "the {word} gcp project with the following properties:"`)
}, nil)

// Default returns the scenario's project (the first registered).
func Default(sc *core.Scenario) (*Project, error) { return projects.Of(sc).Default() }

// Planned returns, for each planned scenario, the project it registers
// first, so packs can prepare what a run's steps will need before the
// scenarios start. Scenarios that register none are left out.
func Planned(s *core.Suite, plan *core.Plan) map[string]*Project {
	out := map[string]*Project{}
	for _, sc := range plan.Scenarios {
		for _, st := range sc.Steps {
			if st.Definition != "gcp-core.project" {
				continue
			}
			if p, err := Parse(s, st.Args[0].Raw, st.Table); err == nil {
				out[sc.ID] = p
			}
			break
		}
	}
	return out
}

// Client returns a client of the project shared by the run: made once with
// open, closed when the run ends.
func Client[C interface{ Close() error }](ctx context.Context, s *core.Suite, pack string, p *Project, open func(context.Context) (C, error)) (C, error) {
	return core.Cached(s, pack+"/client/"+p.Key(), func() (C, error) {
		c, err := open(context.WithoutCancel(ctx))
		if err != nil {
			return c, err
		}
		s.OnClose(func(context.Context) error { return c.Close() })
		return c, nil
	})
}
