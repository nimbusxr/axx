// Package mail is the mail pack: the emails the services under test send,
// read from a mailbox: Mailpit's, through its API, or any mailbox over POP3
// or IMAP, a real one included.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "mail"

const since = "0.1.5"

const packDoc = `Check the emails your services send: who they went to and from, their subject, their text, their HTML, their attachments and their headers.

Your service sends its mail as it always does, by SMTP, to the mail server of your test environment: smtp4dev, Mailpit or any other. axx reads the mailbox that catches it. Register it once, usually in the ` + "`Background`" + `; every mail step of the scenario uses it:

` + "```gherkin" + `
Given the shops mailbox with the following properties:
  | url | http://localhost:8025 |
` + "```" + `

- **Any mailbox, a real one included,** is read over POP3 (` + "`pop3://`" + `, ` + "`pop3s://`" + `) or IMAP (` + "`imap://`" + `, ` + "`imaps://`" + `): smtp4dev, GreenMail, Inbucket, Dovecot, a staging inbox.
- **Mailpit** is read through its API (` + "`http://`" + ` or ` + "`https://`" + `).
- **A check only looks at the emails that arrived since its scenario started,** and waits for one that meets it: 10 seconds, or ` + "`within {duration}`" + `. For a run, axx reads each mailbox the checks name from when the services are up, and never deletes or marks what it reads.
- **Scenarios share the mailbox,** so each checks its own mail, by a recipient or a subject unique to it.
- **Refused mail** is a stub of the mail server's, as WireMock's mappings are for HTTP: smtp4dev refuses the recipients its ` + "`RecipientValidationExpression`" + ` says to, with the code it gives, and a scenario that uses such a recipient sees how your service copes.
- **Mail sent through a provider's HTTP API** (SendGrid, SES, Mailgun, Postmark) is not in a mailbox: mock the provider with WireMock and check the request with the mock pack.
- **Secrets stay secret:** the password, and ` + "`${env:..}`" + ` values, are masked in logs and failures.`

// Pack returns the mail pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Preparer    = pack{}
	_ core.Initializer = pack{}
)

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: []core.StepDef{
		{
			ID: Name + ".mailbox", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} mailbox with the following properties:",
			Doc: "Register the mailbox the steps read.\n\n" +
				"- The first mailbox registered is the one the scenario's mail steps use.\n" +
				"- Values expand `${env:..}` and `${sys:..}`; the password is masked.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "where the mailbox is: Mailpit's address, `http://localhost:8025`, or `pop3://`, `pop3s://`, `imap://` or `imaps://` and the mail server's host", Required: true},
					{Name: "username", Takes: "the user axx signs in as: for POP3 and IMAP, and for a Mailpit behind a password"},
					{Name: "password", Takes: "its password, like `${env:MAILBOX_PASSWORD}`"},
					{Name: "folder", Takes: "the IMAP folder", Default: "INBOX"},
				},
			},
			Examples: []string{
				"Given the shops mailbox with the following properties:\n" +
					"  | url | http://localhost:8025 |",
				"Given the shops mailbox with the following properties:\n" +
					"  | url      | imaps://imap.parcels.example   |\n" +
					"  | username | shop-notifications@parcels.example |\n" +
					"  | password | ${env:MAILBOX_PASSWORD}        |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				m, err := parse0(a.String(0), a.Table, func(v string) string { return secrets.Expand(sc, v) })
				if err != nil {
					return err
				}
				secrets.Keep(sc, m.password)
				if err := mailboxes.Of(sc).Add(m.name, m); err != nil {
					return err
				}
				sc.Log("registered the %s mailbox: %s", m.name, secrets.Mask(sc, redact(m.url)))
				return nil
			},
		},
		{
			ID: Name + ".received", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} mailbox has an email where:",
			Doc: "Check that the mailbox has an email with those values, arrived since the scenario started.\n\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
				"- Addresses compare without regard to case; `text` and `html` are text the body contains, with runs of spaces and line breaks as one space.",
			Table: &core.TableDoc{
				Columns: []string{"field", "value"},
				Rows: []core.TableRow{
					{Name: "to", Takes: "an address the email went to"},
					{Name: "cc", Takes: "an address it was copied to"},
					{Name: "from", Takes: "the address it came from"},
					{Name: "subject", Takes: "its subject, all of it"},
					{Name: "text", Takes: "text its plain-text body contains"},
					{Name: "html", Takes: "text its HTML body contains, tags and all"},
					{Name: "attachment", Takes: "the file name of an attachment"},
					{Name: "header <name>", Takes: "a header's value: its name after `header `"},
				},
			},
			Examples: []string{"Then within 10s the shops mailbox has an email where:\n" +
				"  | to         | orders@hawthorn-home.example |\n" +
				"  | subject    | Parcel PX-MAIL-9701 registered |\n" +
				"  | attachment | PX-MAIL-9701-label.zpl       |"},
			Run: expect,
		},
	}}
}

type mailbox struct {
	name, url, username, password, folder string
}

func (m *mailbox) key() string { return m.url + "|" + m.username + "|" + m.folder }

func parse0(name string, t *core.Table, expand func(string) string) (*mailbox, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	m := &mailbox{name: name, folder: "INBOX"}
	for _, p := range pairs {
		v := strings.TrimSpace(expand(p.Value))
		switch p.Key {
		case "url":
			m.url = v
		case "username":
			m.username = v
		case "password":
			m.password = v
		case "folder":
			m.folder = v
		default:
			return nil, fmt.Errorf("unknown mailbox property %q (supported: url, username, password, folder)", p.Key)
		}
	}
	if m.url == "" {
		return nil, errors.New(`the mailbox property "url" is required`)
	}
	u, err := url.Parse(m.url)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("a mailbox's url is http(s):// for Mailpit, or pop3(s):// or imap(s)://, and a host, not %q", redact(m.url))
	}
	if u.User != nil && m.username == "" {
		m.username = u.User.Username()
		m.password, _ = u.User.Password()
	}
	if _, err := newSource(m); err != nil {
		return nil, err
	}
	if strings.HasPrefix(u.Scheme, "pop3") || strings.HasPrefix(u.Scheme, "imap") {
		if m.username == "" {
			return nil, errors.New("a POP3 or IMAP mailbox needs a username and a password")
		}
	}
	return m, nil
}

var userinfo = regexp.MustCompile(`//([^/@:]*):[^/@]*@`)

// redact leaves a URL's password out.
func redact(raw string) string { return userinfo.ReplaceAllString(raw, "//$1:xxxxx@") }

var mailboxes = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*mailbox] {
	return core.NewServices[*mailbox]("Mailbox",
		`No mailbox is registered in this scenario; register one with "the {word} mailbox with the following properties:"`).RegisteredBy("the {word} mailbox with the following properties:")
}, nil)

// ---- reading, for the run ----

// reader reads a mailbox for the whole run.
type reader struct {
	mu     sync.Mutex
	emails []*email
	err    error
}

const pollEvery = 500 * time.Millisecond

// listen starts reading a mailbox for the run, once.
func listen(s *core.Suite, m *mailbox) (*reader, error) {
	return core.Cached(s, Name+"/reader/"+m.key(), func() (*reader, error) {
		src, err := newSource(m)
		if err != nil {
			return nil, err
		}
		// A few minutes back: a reader that starts late still sees the
		// mail of a scenario that started first.
		since := time.Now().Add(-5 * time.Minute)
		r := &reader{}
		first, err := src.fetch(context.Background(), since)
		if err != nil {
			_ = src.close()
			return nil, fmt.Errorf("cannot read the %s mailbox at %s: %w", m.name, redact(m.url), err)
		}
		r.emails = first
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollEvery):
				}
				got, err := src.fetch(ctx, since)
				r.mu.Lock()
				r.emails = append(r.emails, got...)
				r.err = err
				r.mu.Unlock()
			}
		}()
		s.OnClose(func(context.Context) error {
			cancel()
			<-done
			return src.close()
		})
		return r, nil
	})
}

// since returns the emails that arrived at or after t.
func (r *reader) since(t time.Time) ([]*email, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*email
	for _, e := range r.emails {
		if !e.Received.Before(t) {
			out = append(out, e)
		}
	}
	return out, r.err
}

// Prepare plans reading the mailboxes the run's checks name.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	d := cloudstep.DeferredFor(s, Name)
	for _, sc := range plan.Scenarios {
		var m *mailbox
		for _, st := range sc.Steps {
			if st.Definition == Name+".mailbox" && m == nil {
				m, _ = parse0(st.Args[0].Raw, st.Table, s.Interpolate)
			}
			if st.Definition != Name+".received" || m == nil {
				continue
			}
			mb := m
			d.Add(mb.key(), func(context.Context) error {
				_, err := listen(s, mb)
				return err
			})
		}
	}
	return nil
}

// Init starts reading the planned mailboxes, now that the services run.
func (pack) Init(ctx context.Context, s *core.Suite) error {
	return cloudstep.DeferredFor(s, Name).Run(ctx)
}

// ---- checks ----

func expect(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	rows := make([][2]string, len(pairs))
	for i, p := range pairs {
		rows[i] = [2]string{p.Key, secrets.Expand(sc, p.Value)}
	}
	if _, err := (&email{}).matches(rows); err != nil && strings.HasPrefix(err.Error(), "unknown email row") {
		return err
	}
	m, err := mailboxes.Of(sc).Default()
	if err != nil {
		return err
	}
	r, err := listen(sc.Suite(), m)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	d := cloudstep.Wait(a, 0)
	return secrets.Hide(sc, cloudstep.Poll(sc, d, func() (bool, string, error) {
		emails, err := r.since(sc.Started())
		if err != nil && len(emails) == 0 {
			return false, "", fmt.Errorf("reading the %s mailbox failed: %w", m.name, err)
		}
		shown := make([]string, 0, len(emails))
		for _, e := range emails {
			ok, err := e.matches(rows)
			if err != nil {
				return false, "", err
			}
			if ok {
				return true, "", nil
			}
			shown = append(shown, e.describe())
		}
		n := "emails"
		if len(shown) == 1 {
			n = "email"
		}
		return false, fmt.Sprintf("No email in the %s mailbox met the conditions within %s. It received %s", m.name, d,
			cloudstep.Shown(n+" since the scenario started", shown, 10)), nil
	}))
}
