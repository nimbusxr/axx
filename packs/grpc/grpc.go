// Package grpc is the grpc pack: calls to gRPC services, their status,
// answers, metadata and streamed messages, read with the services' protos
// or their reflection.
package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/protoload"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "grpc"

const since = "0.1.5"

// defaultTimeout is a unary call's deadline unless the service's timeout
// says otherwise.
const defaultTimeout = 10 * time.Second

const packDoc = `Call gRPC services: unary calls and server streams, with requests built from JSON files or tables, and check their status, their answers, their metadata and the messages they stream.

Register the service once, usually in the ` + "`Background`" + `:

` + "```gherkin" + `
Given the tracking grpc service with the following properties:
  | address | localhost:8410 |
` + "```" + `

- **The contract is the service's proto.** A ` + "`proto`" + ` row names its ` + "`.proto`" + ` file (compiled by axx, with its imports) or a descriptor set (` + "`protoc --descriptor_set_out --include_imports`" + `); without one, axx reads the service's descriptors through its server reflection. Requests are built with them, so a field the request message does not have fails the step, and answers are read with them.
- **Methods** are named ` + "`package.Service/Method`" + `, as grpcurl names them; ` + "`Service/Method`" + ` or ` + "`Method`" + ` do when only one of the service's methods has that name.
- **Requests and answers are proto JSON:** fields by their JSON names (` + "`lastScan.location`" + `) or their proto names, enums by name, 64-bit numbers and bytes as proto JSON has them. A request's table sets paths into it, as the REST pack's request properties do.
- **The checks read the service's last call** in the scenario. A call that answers another status than OK does not fail the step that makes it: check its status.
- **Server streams** belong to the scenario, which closes them when it ends. Their messages are checked as they come; their status once they end.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties, such as a token in ` + "`header.authorization`" + `, are masked in logs and failures, and ` + "`${token:..}`" + ` names a token of the scenario.`

// Pack returns the grpc pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps()}
}

// service is a gRPC service a scenario registered.
type service struct {
	name, address string
	tls           bool
	proto         string // the resolved proto file or descriptor set; "" reads the reflection
	headers       metadata.MD
	timeout       time.Duration

	mu   sync.Mutex
	last *call
}

func (s *service) key() string { return fmt.Sprintf("%s|%t", s.address, s.tls) }

var services = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*service] {
	all := core.NewServices[*service]("gRPC service",
		`No gRPC service is registered in this scenario; register one with "the {word} grpc service with the following properties:"`)
	sc.Describe(Name, func() any { return describe(sc, all) })
	return all
}, func(_ *core.Scenario, all *core.Services[*service]) error {
	for _, s := range all.All() {
		s.mu.Lock()
		if s.last != nil {
			s.last.stop()
		}
		s.mu.Unlock()
	}
	return nil
})

func register(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the grpc service property "address" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &service{name: name, headers: metadata.MD{}, timeout: defaultTimeout}
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		v = strings.TrimSpace(v)
		switch {
		case p.Key == "address":
			s.address = v
		case p.Key == "proto":
			if s.proto, err = sc.Suite().ResolvePath(v); err != nil {
				return fmt.Errorf("the %s grpc service's proto: %w", name, err)
			}
		case p.Key == "tls":
			switch strings.ToLower(v) {
			case "true":
				s.tls = true
			case "false":
				s.tls = false
			default:
				return fmt.Errorf("the %s grpc service's tls is true or false, not %q", name, p.Value)
			}
		case strings.HasPrefix(p.Key, "header."):
			s.headers.Append(strings.ToLower(strings.TrimPrefix(p.Key, "header.")), v)
		case p.Key == "timeout":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return fmt.Errorf("the %s grpc service's timeout %q is not a duration, like 5s or 1m", name, p.Value)
			}
			s.timeout = d
		default:
			return fmt.Errorf("unknown grpc service property %q (supported: address, proto, tls, header.<name>, timeout)", p.Key)
		}
	}
	if s.address == "" {
		return errors.New(`the grpc service property "address" is required`)
	}
	if err := services.Of(sc).Add(name, s); err != nil {
		return err
	}
	how := "its reflection"
	if s.proto != "" {
		how = filepath.Base(s.proto)
	}
	sc.Log("registered the %s grpc service: %s (read with %s)", name, s.address, how)
	return nil
}

func get(sc *core.Scenario, name string) (*service, error) {
	s, err := services.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no grpc service named %q in this scenario; register it first with \"the %s grpc service with the following properties:\"", name, name)
	}
	return s, nil
}

// conn is the run's connection to the service's address.
func (s *service) conn(suite *core.Suite) (*grpclib.ClientConn, error) {
	return core.Cached(suite, Name+"/conn/"+s.key(), func() (*grpclib.ClientConn, error) {
		creds := insecure.NewCredentials()
		if s.tls {
			creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		}
		c, err := grpclib.NewClient(s.address, grpclib.WithTransportCredentials(creds))
		if err != nil {
			return nil, fmt.Errorf("cannot connect to %s: %w", s.address, err)
		}
		suite.OnClose(func(context.Context) error { return c.Close() })
		return c, nil
	})
}

// descriptors are the service's protos: its proto file or descriptor set,
// or what its reflection says, read once per run.
func (s *service) descriptors(sc *core.Scenario) (*protoload.Set, error) {
	cache, _ := core.Cached(sc.Suite(), Name+"/descriptors", func() (*descriptorCache, error) {
		return &descriptorCache{sets: map[string]*protoload.Set{}}, nil
	})
	key := "proto|" + s.proto
	if s.proto == "" {
		key = "reflection|" + s.key()
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if set, ok := cache.sets[key]; ok {
		return set, nil
	}
	var set *protoload.Set
	var err error
	switch {
	case strings.HasSuffix(s.proto, ".proto"):
		importPaths := []string{filepath.Dir(s.proto)}
		if dir := sc.Suite().ProjectDir(); dir != "" {
			importPaths = append(importPaths, dir)
		}
		if set, err = protoload.Compile(filepath.Base(s.proto), importPaths); err != nil {
			err = fmt.Errorf("cannot compile %s: %w", filepath.Base(s.proto), err)
		}
	case s.proto != "":
		set, err = protoload.DescriptorSet(s.proto, filepath.Base(s.proto))
	default:
		var conn *grpclib.ClientConn
		if conn, err = s.conn(sc.Suite()); err == nil {
			ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
			set, err = reflect(ctx, conn, s.address)
			cancel()
		}
	}
	if err != nil {
		// Not kept: the service may answer the next scenario.
		return nil, err
	}
	cache.sets[key] = set
	return set, nil
}

type descriptorCache struct {
	mu   sync.Mutex
	sets map[string]*protoload.Set
}

// describe is the scenario's last calls, for failure reports.
func describe(sc *core.Scenario, all *core.Services[*service]) any {
	out := map[string]any{}
	for _, s := range all.All() {
		s.mu.Lock()
		c := s.last
		s.mu.Unlock()
		if c != nil {
			out[s.name] = c.describe(sc)
		}
	}
	return out
}
