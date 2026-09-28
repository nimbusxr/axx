package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// email is an email as the checks see it.
type email struct {
	From        string // the sender's address
	To, Cc      []string
	Subject     string
	Text, HTML  string
	Attachments []string // file names
	Header      mail.Header
	Received    time.Time
}

// decoder decodes the encoded words of headers, in UTF-8, US-ASCII and the
// Latin charsets mail clients use.
var decoder = &mime.WordDecoder{CharsetReader: charsetReader}

func charsetReader(charset string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1":
		return charmap.ISO8859_1.NewDecoder().Reader(r), nil
	case "iso-8859-15":
		return charmap.ISO8859_15.NewDecoder().Reader(r), nil
	case "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(r), nil
	}
	return r, nil // UTF-8, US-ASCII, and the rest as they are
}

// parse reads a raw email (RFC 5322 and MIME).
func parse(raw []byte, received time.Time) (*email, error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	e := &email{Header: m.Header, Received: received}
	e.Subject = decodeHeader(m.Header.Get("Subject"))
	if from, err := decoder.DecodeHeader(m.Header.Get("From")); err == nil {
		e.From = firstAddress(from)
	}
	e.To = addresses(m.Header, "To")
	e.Cc = addresses(m.Header, "Cc")
	if err := e.walk(m.Header, m.Body); err != nil {
		return nil, err
	}
	return e, nil
}

func decodeHeader(v string) string {
	if d, err := decoder.DecodeHeader(v); err == nil {
		return d
	}
	return v
}

// firstAddress is the address of a header's first mailbox.
func firstAddress(v string) string {
	if a, err := mail.ParseAddress(v); err == nil {
		return strings.ToLower(a.Address)
	}
	return strings.ToLower(strings.TrimSpace(v))
}

func addresses(h mail.Header, name string) []string {
	list, err := (&mail.AddressParser{WordDecoder: decoder}).ParseList(h.Get(name))
	if err != nil {
		return nil
	}
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = strings.ToLower(a.Address)
	}
	return out
}

// part is what walk needs of a part's headers.
type part interface{ Get(string) string }

// walk reads a part: its text, its HTML, its attachments.
func (e *email) walk(h part, body io.Reader) error {
	mediaType, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if err := e.walk(p.Header, p); err != nil {
				return err
			}
		}
	}
	disposition, dparams, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	if disposition == "attachment" || (name != "" && disposition != "inline") {
		e.Attachments = append(e.Attachments, decodeHeader(name))
		return nil
	}
	content, err := io.ReadAll(decoded(h.Get("Content-Transfer-Encoding"), body))
	if err != nil {
		return err
	}
	if r, err := charsetReader(params["charset"], bytes.NewReader(content)); err == nil {
		content, _ = io.ReadAll(r)
	}
	switch {
	case mediaType == "text/plain" && e.Text == "":
		e.Text = string(content)
	case mediaType == "text/html" && e.HTML == "":
		e.HTML = string(content)
	}
	return nil
}

func decoded(encoding string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &newlineSkipper{r})
	}
	return r
}

// newlineSkipper drops the line breaks of base64 text.
type newlineSkipper struct{ r io.Reader }

func (n *newlineSkipper) Read(p []byte) (int, error) {
	k, err := n.r.Read(p)
	j := 0
	for _, c := range p[:k] {
		if c != '\r' && c != '\n' {
			p[j] = c
			j++
		}
	}
	return j, err
}

// ---- conditions ----

// rowNames are the rows an email check knows.
var rowNames = "to, from, cc, subject, text, html, attachment, header <name>"

// matches reports whether the email meets every row.
func (e *email) matches(rows [][2]string) (bool, error) {
	for _, r := range rows {
		name, want := r[0], r[1]
		var ok bool
		switch {
		case name == "to":
			ok = hasAddress(e.To, want)
		case name == "cc":
			ok = hasAddress(e.Cc, want)
		case name == "from":
			ok = e.From == firstAddress(want)
		case name == "subject":
			ok = e.Subject == want
		case name == "text":
			ok = strings.Contains(collapse(e.Text), collapse(want))
		case name == "html":
			ok = strings.Contains(collapse(e.HTML), collapse(want))
		case name == "attachment":
			ok = contains(e.Attachments, want)
		case strings.HasPrefix(name, "header "):
			ok = decodeHeader(e.Header.Get(strings.TrimSpace(strings.TrimPrefix(name, "header ")))) == want
		default:
			return false, fmt.Errorf("unknown email row %q (supported: %s)", name, rowNames)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func hasAddress(list []string, want string) bool {
	return contains(list, firstAddress(want))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// collapse makes runs of spaces and line breaks one space.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// describe is an email in a failure report.
func (e *email) describe() string {
	att := ""
	if len(e.Attachments) > 0 {
		names := append([]string(nil), e.Attachments...)
		sort.Strings(names)
		att = fmt.Sprintf(" (attached: %s)", strings.Join(names, ", "))
	}
	return fmt.Sprintf("from %s to %s: %q%s", e.From, strings.Join(append(append([]string{}, e.To...), e.Cc...), ", "), e.Subject, att)
}
