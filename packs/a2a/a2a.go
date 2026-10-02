// Package a2a is the a2a pack: messages to A2A agents (Agent2Agent protocol)
// as another agent sends them, over JSON-RPC, HTTP+JSON or gRPC, and their
// answers: the tasks they run, with their states and artifacts, as they
// answer or as they stream.
package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "a2a"

const since = "0.1.5"

const defaultTimeout = 30 * time.Second

// Pack returns the a2a pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps()}
}

// agent is an A2A agent a scenario registered, and what it answered the
// scenario: its last reply, or its last task.
type agent struct {
	name      string
	url       string
	cardRow   string
	transport a2a.TransportProtocol
	headers   http.Header
	timeout   time.Duration
	push      *a2a.PushConfig
	card      *a2a.AgentCard
	client    *a2aclient.Client

	mu      sync.Mutex
	reply   *a2a.Message // an answer that is a message, not a task
	task    *a2a.Task    // the task the scenario's messages run
	context string       // the conversation the messages continue
	updates []update     // what the stream sent, in order
	stream  *streaming   // the stream of the last message streamed
}

// streaming is a stream the scenario opened, which ends with it.
type streaming struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // why it ended, when it failed
}

var agents = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*agent] {
	all := core.NewServices[*agent]("A2A agent",
		`No A2A agent is registered in this scenario; register one with "the {word} a2a agent with the following properties:"`).RegisteredBy("the {word} a2a agent with the following properties:")
	sc.Describe(Name, func() any { return describe(sc, all) })
	return all
}, func(_ *core.Scenario, all *core.Services[*agent]) error {
	var errs []error
	for _, a := range all.All() {
		a.mu.Lock()
		s := a.stream
		a.mu.Unlock()
		if s != nil {
			s.cancel()
			<-s.done
		}
		errs = append(errs, a.client.Destroy())
	}
	return errors.Join(errs...)
})

func register(sc *core.Scenario, args core.Args) error {
	name := args.String(0)
	if args.Table == nil {
		return errors.New(`the a2a agent property "url" or "card" is required`)
	}
	pairs, err := args.Table.Pairs()
	if err != nil {
		return err
	}
	a := &agent{name: name, headers: http.Header{}, timeout: defaultTimeout}
	var pushURL, pushToken string
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		v = strings.TrimSpace(v)
		switch {
		case p.Key == "url":
			a.url = v
		case p.Key == "card":
			a.cardRow = v
		case p.Key == "transport":
			switch t := a2a.TransportProtocol(strings.ToUpper(v)); t {
			case a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON, a2a.TransportProtocolGRPC:
				a.transport = t
			default:
				return fmt.Errorf("the %s a2a agent's transport is JSONRPC, HTTP+JSON or GRPC, not %q", name, p.Value)
			}
		case strings.HasPrefix(p.Key, "header."):
			a.headers.Add(strings.TrimPrefix(p.Key, "header."), v)
		case p.Key == "timeout":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return fmt.Errorf("the %s a2a agent's timeout %q is not a duration, like 30s or 2m", name, p.Value)
			}
			a.timeout = d
		case p.Key == "push url":
			pushURL = v
		case p.Key == "push token":
			pushToken = v
		default:
			return fmt.Errorf("unknown a2a agent property %q (supported: url, card, transport, header.<name>, timeout, push url, push token)", p.Key)
		}
	}
	if a.url == "" && a.cardRow == "" {
		return fmt.Errorf(`the %s a2a agent needs a "url" (its card is read at /.well-known/agent-card.json) or a "card"`, name)
	}
	if a.url != "" && !strings.HasPrefix(a.url, "http://") && !strings.HasPrefix(a.url, "https://") {
		return fmt.Errorf("the %s a2a agent's url is http:// or https://, not %q", name, a.url)
	}
	if pushToken != "" && pushURL == "" {
		return fmt.Errorf(`the %s a2a agent's push token goes with a "push url"`, name)
	}
	if pushURL != "" {
		a.push = &a2a.PushConfig{URL: pushURL, Token: pushToken}
	}
	if err := a.connect(sc); err != nil {
		return secrets.Hide(sc, err)
	}
	if err := agents.Of(sc).Add(name, a); err != nil {
		_ = a.client.Destroy()
		return err
	}
	sc.Log("registered the %s a2a agent: %s %s", name, a.card.Name, a.card.Version)
	return nil
}

// connect reads the agent's card, checks it, and makes a client of the
// transport the card offers first, or the one the registration asks for.
func (a *agent) connect(sc *core.Scenario) error {
	ctx, cancel := context.WithTimeout(sc.Context(), a.timeout)
	defer cancel()
	var card *a2a.AgentCard
	var err error
	source := a.cardRow
	switch {
	case a.cardRow != "" && !strings.HasPrefix(a.cardRow, "http://") && !strings.HasPrefix(a.cardRow, "https://"):
		// A card file of the project.
		var p string
		if p, err = sc.Suite().ResolvePath(a.cardRow); err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("the %s a2a agent's card %s: %w", a.name, a.cardRow, err)
		}
		if card, err = agentcard.DefaultCardParser(body); err != nil {
			return fmt.Errorf("the %s a2a agent's card %s is not an agent card: %w", a.name, a.cardRow, err)
		}
	default:
		if source == "" {
			// The card is at the root of the agent's origin.
			u, err := url.Parse(a.url)
			if err != nil {
				return fmt.Errorf("the %s a2a agent's url %q: %w", a.name, a.url, err)
			}
			source = u.Scheme + "://" + u.Host + "/.well-known/agent-card.json"
		}
		var opts []agentcard.ResolveOption
		for k, vs := range a.headers {
			for _, v := range vs {
				opts = append(opts, agentcard.WithRequestHeader(k, v))
			}
		}
		if card, err = agentcard.DefaultResolver.Resolve(ctx, source, opts...); err != nil {
			return fmt.Errorf("cannot read the %s a2a agent's card from %s: %w", a.name, source, err)
		}
	}
	if problems := checkCard(card); len(problems) > 0 {
		return core.Failf("the %s a2a agent's card is not an A2A 1.0 agent card:\n- %s", a.name, strings.Join(problems, "\n- "))
	}
	a.card = card
	if a.transport != "" {
		var listed []string
		found := false
		for _, in := range card.SupportedInterfaces {
			listed = append(listed, string(in.ProtocolBinding))
			found = found || in.ProtocolBinding == a.transport
		}
		if !found {
			return fmt.Errorf("the %s a2a agent's card lists no %s interface; it lists %s", a.name, a.transport, strings.Join(listed, ", "))
		}
	}
	config := a2aclient.Config{PushConfig: a.push}
	if a.transport != "" {
		config.PreferredTransports = []a2a.TransportProtocol{a.transport}
	}
	client, err := a2aclient.NewFromCard(ctx, card,
		a2aclient.WithConfig(config),
		a2aclient.WithCallInterceptors(headerInterceptor{headers: a.headers}),
		a2aclient.WithJSONRPCTransport(&http.Client{Transport: streamsOnly{http.DefaultTransport}}),
		a2agrpc.WithGRPCTransport(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		return fmt.Errorf("cannot reach the %s a2a agent: %w", a.name, err)
	}
	a.client = client
	return nil
}

// checkCard is what a card lacks of the fields A2A 1.0 requires.
func checkCard(c *a2a.AgentCard) []string {
	var out []string
	for _, f := range []struct {
		name  string
		empty bool
	}{
		{"name", c.Name == ""},
		{"description", c.Description == ""},
		{"version", c.Version == ""},
		{"supportedInterfaces", len(c.SupportedInterfaces) == 0},
		{"defaultInputModes", len(c.DefaultInputModes) == 0},
		{"defaultOutputModes", len(c.DefaultOutputModes) == 0},
	} {
		if f.empty {
			out = append(out, "it has no "+f.name)
		}
	}
	for i, in := range c.SupportedInterfaces {
		if in.URL == "" || in.ProtocolBinding == "" {
			out = append(out, fmt.Sprintf("its supportedInterfaces[%d] has no url or protocolBinding", i))
		}
	}
	return out
}

// streamsOnly fails a stream that the agent answered with something else
// than an event stream, such as a JSON-RPC error: the JSON-RPC client reads
// such an answer as a stream that ended without an event.
type streamsOnly struct{ next http.RoundTripper }

func (s streamsOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := s.next.RoundTrip(req)
	if err != nil || req.Header.Get("Accept") != "text/event-stream" || res.StatusCode != http.StatusOK {
		return res, err
	}
	kind := res.Header.Get("Content-Type")
	if strings.HasPrefix(kind, "text/event-stream") {
		return res, nil
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var rpc struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &rpc) == nil && rpc.Error != nil {
		return nil, fmt.Errorf("the agent answered the stream with the JSON-RPC error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	return nil, fmt.Errorf("the agent answered the stream with %q, not an event stream: %s", kind, strings.TrimSpace(string(body)))
}

// headerInterceptor sends the registration's headers with every call.
type headerInterceptor struct {
	headers http.Header
}

func (h headerInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	for k, vs := range h.headers {
		req.ServiceParams.Append(k, vs...)
	}
	return ctx, nil, nil
}

func (headerInterceptor) After(context.Context, *a2aclient.Response) error { return nil }

func get(sc *core.Scenario, name string) (*agent, error) {
	a, err := agents.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no a2a agent named %q in this scenario; register it first with \"the %s a2a agent with the following properties:\"", name, name)
	}
	return a, nil
}

func describe(sc *core.Scenario, all *core.Services[*agent]) any {
	out := map[string]any{}
	for _, a := range all.All() {
		a.mu.Lock()
		d := map[string]any{"card": a.card.Name}
		if a.reply != nil {
			d["reply"] = secrets.Mask(sc, messageText(a.reply))
		}
		if a.task != nil {
			d["task"] = map[string]any{"state": stateWord(a.task.Status.State), "message": secrets.Mask(sc, statusText(a.task)), "artifacts": len(a.task.Artifacts)}
		}
		if len(a.updates) > 0 {
			b, _ := json.Marshal(a.updates)
			d["stream"] = secrets.Mask(sc, string(b))
		}
		a.mu.Unlock()
		out[a.name] = d
	}
	return out
}

const packDoc = `Send messages to A2A agents (Agent2Agent protocol) as another agent sends them, and check what they answer: the tasks they run, with their states and their artifacts, as they answer or as they stream.

` + "```gherkin" + `
Given the parcels a2a agent with the following properties:
  | url | http://localhost:8400 |
When a message is sent to the parcels a2a agent:
  """
  Where is PX-A2A-9201?
  """
Then the parcels a2a agent's task is completed
And the parcels a2a agent's answer contains 'out for delivery'
` + "```" + `

- **The card is the contract:** it is read at ` + "`/.well-known/agent-card.json`" + ` (or from a file), checked for what A2A 1.0 requires, and says how to reach the agent: JSON-RPC, HTTP+JSON or gRPC.
- **A conversation:** a message starts one; a reply continues the agent's last task, which is how a task that asks for input (` + "`input-required`" + `) gets it.
- **Streams belong to the scenario,** which ends them when it ends. The checks wait (10 seconds, or ` + "`within {duration}`" + `) for what the stream has not sent yet.
- **States are written as people say them:** ` + "`submitted`, `working`, `completed`, `input-required`, `auth-required`, `failed`, `canceled`, `rejected`" + `.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures, and ` + "`${token:..}`" + ` names a token of the scenario.`
