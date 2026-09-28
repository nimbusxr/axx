package mail

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

// source reads a mailbox: the emails that arrived since it last looked.
type source interface {
	// fetch returns the emails that arrived since the last fetch; the
	// first fetch, those that arrived after since.
	fetch(ctx context.Context, since time.Time) ([]*email, error)
	close() error
}

func newSource(m *mailbox) (source, error) {
	u, err := url.Parse(m.url)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return &mailpit{
			base: strings.TrimRight(m.url, "/"), user: m.username, password: m.password, seen: map[string]bool{},
			http: &http.Client{Timeout: 10 * time.Second},
		}, nil
	case "pop3", "pop3s":
		return &pop3{
			addr: hostPort(u, map[string]string{"pop3": "110", "pop3s": "995"}), tls: u.Scheme == "pop3s",
			host: u.Hostname(), user: m.username, password: m.password,
		}, nil
	case "imap", "imaps":
		return &imapSource{
			addr: hostPort(u, map[string]string{"imap": "143", "imaps": "993"}), tls: u.Scheme == "imaps",
			user: m.username, password: m.password, folder: m.folder,
		}, nil
	}
	return nil, fmt.Errorf("a mailbox's url is http(s):// for Mailpit, pop3(s):// or imap(s)://, not %s://", u.Scheme)
}

func hostPort(u *url.URL, ports map[string]string) string {
	if u.Port() != "" {
		return u.Host
	}
	return net.JoinHostPort(u.Hostname(), ports[u.Scheme])
}

// ---- Mailpit, through its API ----

type mailpit struct {
	base, user, password string
	http                 *http.Client
	seen                 map[string]bool
}

func (m *mailpit) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return nil, err
	}
	if m.user != "" {
		req.SetBasicAuth(m.user, m.password)
	}
	res, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Mailpit API answered %s %d: %s", path, res.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func (m *mailpit) fetch(ctx context.Context, since time.Time) ([]*email, error) {
	type summary struct {
		ID      string
		Created time.Time
	}
	var fresh []summary
	// Newest first, a page at a time, down to what was seen or is too old.
	for start := 0; start < 1000; start += 100 {
		b, err := m.get(ctx, "/api/v1/messages?limit=100&start="+strconv.Itoa(start))
		if err != nil {
			return nil, err
		}
		var page struct{ Messages []summary }
		if err := json.Unmarshal(b, &page); err != nil {
			return nil, fmt.Errorf("reading Mailpit's message list: %w", err)
		}
		done := len(page.Messages) < 100
		for _, s := range page.Messages {
			if m.seen[s.ID] || s.Created.Before(since) {
				done = true
				break
			}
			fresh = append(fresh, s)
		}
		if done {
			break
		}
	}
	var out []*email
	for i := len(fresh) - 1; i >= 0; i-- { // oldest first
		s := fresh[i]
		raw, err := m.get(ctx, "/api/v1/message/"+url.PathEscape(s.ID)+"/raw")
		if err != nil {
			return out, err
		}
		m.seen[s.ID] = true
		e, err := parse(raw, s.Created)
		if err != nil {
			continue // not an email the checks can read
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *mailpit) close() error { return nil }

// ---- POP3 ----

type pop3 struct {
	addr, host, user, password string
	tls                        bool
	seen                       map[string]bool // nil until the first fetch
}

// fetch signs in, lists the messages by their unique IDs and reads the
// new ones. POP3 says nothing of when a message arrived: the first fetch
// takes what is there already as old.
func (p *pop3) fetch(ctx context.Context, _ time.Time) ([]*email, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", p.addr)
	if err != nil {
		return nil, err
	}
	if p.tls {
		conn = tls.Client(conn, &tls.Config{ServerName: p.host, MinVersion: tls.VersionTLS12})
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	t := textproto.NewConn(conn)
	cmd := func(format string, args ...any) (string, error) {
		if format != "" {
			if err := t.PrintfLine(format, args...); err != nil {
				return "", err
			}
		}
		line, err := t.ReadLine()
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(line, "+OK") {
			return "", fmt.Errorf("the POP3 server answered %q", line)
		}
		return line, nil
	}
	if _, err := cmd(""); err != nil {
		return nil, err
	}
	if _, err := cmd("USER %s", p.user); err != nil {
		return nil, err
	}
	if _, err := cmd("PASS %s", p.password); err != nil {
		return nil, errors.New("the POP3 server refused the username and password")
	}
	defer func() { _, _ = cmd("QUIT") }()
	if _, err := cmd("UIDL"); err != nil {
		return nil, err
	}
	lines, err := t.ReadDotLines()
	if err != nil {
		return nil, err
	}
	first := p.seen == nil
	if first {
		p.seen = map[string]bool{}
	}
	var out []*email
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) != 2 || p.seen[f[1]] {
			continue
		}
		p.seen[f[1]] = true
		if first {
			continue
		}
		if _, err := cmd("RETR %s", f[0]); err != nil {
			return out, err
		}
		raw, err := io.ReadAll(t.DotReader())
		if err != nil {
			return out, err
		}
		if e, err := parse(raw, time.Now()); err == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (p *pop3) close() error { return nil }

// ---- IMAP ----

type imapSource struct {
	addr, user, password, folder string
	tls                          bool
	c                            *imapclient.Client
	seen                         map[uint32]bool
}

func (s *imapSource) connect() error {
	var err error
	if s.tls {
		s.c, err = imapclient.DialTLS(s.addr, nil)
	} else {
		s.c, err = imapclient.Dial(s.addr)
	}
	if err != nil {
		return err
	}
	s.c.Timeout = 30 * time.Second
	if err := s.c.Login(s.user, s.password); err != nil {
		_ = s.c.Logout()
		s.c = nil
		return fmt.Errorf("the IMAP server refused the username and password: %w", err)
	}
	if _, err := s.c.Select(s.folder, true); err != nil {
		_ = s.c.Logout()
		s.c = nil
		return fmt.Errorf("the IMAP folder %s: %w", s.folder, err)
	}
	return nil
}

// fetch reads the messages that arrived (INTERNALDATE) since since.
func (s *imapSource) fetch(_ context.Context, since time.Time) ([]*email, error) {
	if s.c == nil {
		if err := s.connect(); err != nil {
			return nil, err
		}
	}
	if s.seen == nil {
		s.seen = map[uint32]bool{}
	}
	if err := s.c.Noop(); err != nil { // see what arrived since the last look
		s.c = nil
		return nil, err
	}
	criteria := imap.NewSearchCriteria()
	criteria.Since = since.Add(-24 * time.Hour) // SINCE is a date: narrow by the time below
	uids, err := s.c.UidSearch(criteria)
	if err != nil {
		s.c = nil
		return nil, err
	}
	set := new(imap.SeqSet)
	for _, uid := range uids {
		if !s.seen[uid] {
			set.AddNum(uid)
		}
	}
	if set.Empty() {
		return nil, nil
	}
	section := &imap.BodySectionName{Peek: true}
	msgs := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() {
		done <- s.c.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchInternalDate, section.FetchItem()}, msgs)
	}()
	var out []*email
	for m := range msgs {
		s.seen[m.Uid] = true
		if m.InternalDate.Before(since) {
			continue
		}
		body := m.GetBody(section)
		if body == nil {
			continue
		}
		raw, err := io.ReadAll(bufio.NewReader(body))
		if err != nil {
			continue
		}
		// INTERNALDATE has whole seconds: mail that came in the second a
		// scenario started counts as its own.
		if e, err := parse(raw, m.InternalDate.Truncate(time.Second).Add(time.Second-time.Nanosecond)); err == nil {
			out = append(out, e)
		}
	}
	if err := <-done; err != nil {
		s.c = nil
		return out, err
	}
	return out, nil
}

func (s *imapSource) close() error {
	if s.c == nil {
		return nil
	}
	return s.c.Logout()
}
