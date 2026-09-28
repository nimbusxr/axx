// Package contract is how messaging packs have their messages checked
// against the contract a registration names, in its asyncapi row, without
// depending on what checks it: the asyncapi pack provides the checks when a
// project adds it, and a registration naming a contract without it fails.
package contract

import (
	"errors"
	"net/url"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// Row is the registration row that names a contract.
const Row = "asyncapi"

// TableRow documents Row on a registration's table; what names what the
// registration's messages are on ("topics", "queues and exchanges").
func TableRow(what string) core.TableRow {
	return core.TableRow{
		Name:  Row,
		Takes: "an AsyncAPI document, a file of the project or a URL, that the " + what + " follow: every message a scenario sends there, and every message a check finds, is checked against it (with the asyncapi pack)",
	}
}

// Message is a message a scenario sends, or a check found, as its contract
// sees it.
type Message struct {
	// Protocol is the AsyncAPI protocol it travels over: kafka, amqp,
	// amqp1, mqtt, nats, sqs, sns, googlepubsub, servicebus, ws or http.
	Protocol string
	// Addresses are the names of where it goes, which a channel's address
	// can be: a topic, a queue, an exchange and then its routing key.
	Addresses []string
	// Payload is its body; ContentType its media type, when it has one.
	Payload     []byte
	ContentType string
	// Headers are the named values sent with it.
	Headers map[string]string
	// Name is the message's name, when its protocol gives it one: an
	// event stream's event type. A channel's message of that name is the
	// one it is checked against.
	Name string
	// Sent tells a message the scenario sends from one a check found.
	Sent bool
}

// URLAddresses are the addresses of a WebSocket's or an event stream's
// messages: its URL's path, which a channel's address is.
func URLAddresses(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return []string{raw}
	}
	return []string{u.Path}
}

// Checker checks messages against a contract. Check fails when a message
// breaks it at the ERROR level, and logs what it breaks at lower levels.
type Checker interface {
	Check(sc *core.Scenario, m Message) error
}

// Opener opens the contract a registration names.
type Opener func(sc *core.Scenario, source string) (Checker, error)

// ErrNoChecks is Open's error when no pack checks contracts.
var ErrNoChecks = errors.New("the asyncapi row needs the asyncapi pack, which checks messages against AsyncAPI documents: add it with `axx pack add asyncapi`")

type provider struct {
	mu   sync.Mutex
	open Opener
}

func providerOf(s *core.Suite) *provider {
	p, _ := core.Cached(s, "contract/provider", func() (*provider, error) { return &provider{}, nil })
	return p
}

// Provide makes open how the run's registrations open their contracts.
// The asyncapi pack calls it when the run starts.
func Provide(s *core.Suite, open Opener) {
	p := providerOf(s)
	p.mu.Lock()
	p.open = open
	p.mu.Unlock()
}

// Open opens the contract a registration's asyncapi row names.
func Open(sc *core.Scenario, source string) (Checker, error) {
	p := providerOf(sc.Suite())
	p.mu.Lock()
	open := p.open
	p.mu.Unlock()
	if open == nil {
		return nil, ErrNoChecks
	}
	return open(sc, source)
}

// Check checks m when c is a contract; a nil c checks nothing.
func Check(c Checker, sc *core.Scenario, m Message) error {
	if c == nil {
		return nil
	}
	return c.Check(sc, m)
}
