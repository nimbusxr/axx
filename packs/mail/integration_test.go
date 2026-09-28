//go:build integration

package mail

import (
	"net/http"
	"net/smtp"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// send sends an email by SMTP, as the service under test does.
func send(t *testing.T, addr, to, subject string) {
	t.Helper()
	msg := strings.ReplaceAll(strings.Replace(registered, "Parcel PX-9701 registered", subject, 1), "Subject: =?UTF-8?Q?Parcel_PX-9701_registered_=E2=80=93_label?=", "Subject: "+subject)
	msg = strings.Replace(msg, "To: Orders <orders@hawthorn-home.example>", "To: "+to, 1)
	if err := smtp.SendMail(addr, nil, "no-reply@parcels.example", []string{to}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func checkMailbox(t *testing.T, rows [][]string, smtpAddr, to string) {
	t.Helper()
	h := cloudtest.New(t, Pack())
	h.Start(h.Plan(
		cloudtest.PlannedStep{Text: "the shops mailbox with the following properties:", Table: rows},
		cloudtest.PlannedStep{Text: "within 10s the shops mailbox has an email where:", Table: [][]string{{"to", to}}},
	))
	h.OK("the shops mailbox with the following properties:", rows)
	send(t, smtpAddr, to, "Parcel PX-9702 registered")
	h.OK("within 10s the shops mailbox has an email where:", [][]string{
		{"to", to},
		{"from", "no-reply@parcels.example"},
		{"subject", "Parcel PX-9702 registered"},
		{"text", "Your parcel PX-9701 to Jürgen Weiß is registered."},
		{"html", "<b>PX-9701</b>"},
		{"attachment", "PX-9701-label.zpl"},
		{"header X-Parcel-Reference", "PX-9701"},
	})
	_ = h.Fails("within 1s the shops mailbox has an email where:", `No email in the shops mailbox met the conditions within 1s. It received 1 email since the scenario started:`,
		[][]string{{"to", to}, {"subject", "Parcel PX-9703 registered"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	// Another scenario sees none of the first one's mail.
	h.NewScenario()
	h.OK("the shops mailbox with the following properties:", rows)
	_ = h.Fails("within 1s the shops mailbox has an email where:", "It received no emails since the scenario started", [][]string{{"to", to}})
}

func TestMailpit(t *testing.T) {
	addrs := cloudtest.ServerPorts(t, "axllent/mailpit:v1.31.3", []string{"1025", "8025", "1110"}, nil,
		map[string]string{"MP_POP3_AUTH": "axx:mailbox-pass"})
	t.Run("api", func(t *testing.T) {
		checkMailbox(t, [][]string{{"url", "http://" + addrs["8025"]}}, addrs["1025"], "orders@hawthorn-home.example")
	})
	t.Run("pop3", func(t *testing.T) {
		checkMailbox(t, [][]string{{"url", "pop3://" + addrs["1110"]}, {"username", "axx"}, {"password", "mailbox-pass"}},
			addrs["1025"], "orders@wisteria-way.example")
	})
}

func TestGreenMailOverIMAP(t *testing.T) {
	addrs := cloudtest.ServerPorts(t, "greenmail/standalone:2.1.14", []string{"3025", "3143", "8080"}, nil, nil)
	const to = "orders@linden-and-lace.example"
	// GreenMail makes a mailbox when mail first comes, or when its API is asked for one.
	res, err := http.Post("http://"+addrs["8080"]+"/api/user", "application/json",
		strings.NewReader(`{"email": "`+to+`", "login": "`+to+`", "password": "mailbox-pass"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("creating the GreenMail user: %d", res.StatusCode)
	}
	checkMailbox(t, [][]string{{"url", "imap://" + addrs["3143"]}, {"username", to}, {"password", "mailbox-pass"}}, addrs["3025"], to)
}

// smtp4dev, over IMAP: it takes any username and password, and its INBOX
// has every email it received.
func TestSmtp4devOverIMAP(t *testing.T) {
	addrs := cloudtest.ServerPorts(t, "rnwood/smtp4dev:3.15.0", []string{"25", "143"}, nil, nil)
	checkMailbox(t, [][]string{{"url", "imap://" + addrs["143"]}, {"username", "axx"}, {"password", "any"}},
		addrs["25"], "orders@hawthorn-home.example")
}
