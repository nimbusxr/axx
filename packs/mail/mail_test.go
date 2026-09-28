package mail

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

var registered = strings.ReplaceAll(`From: "Parcels" <no-reply@parcels.example>
To: Orders <orders@hawthorn-home.example>
Cc: support@parcels.example
Subject: =?UTF-8?Q?Parcel_PX-9701_registered_=E2=80=93_label?=
X-Parcel-Reference: PX-9701
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="mixed"

--mixed
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Your parcel PX-9701 to J=FCrgen Wei=DF
is registered.
--alt
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: base64

`+base64.StdEncoding.EncodeToString([]byte("<p>Your parcel <b>PX-9701</b> is registered.</p>"))+`
--alt--
--mixed
Content-Type: application/octet-stream; name="PX-9701-label.zpl"
Content-Disposition: attachment; filename="PX-9701-label.zpl"
Content-Transfer-Encoding: base64

XlhBXlha
--mixed--
`, "\n", "\r\n")

func TestAnEmail(t *testing.T) {
	e, err := parse([]byte(registered), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][][2]string{
		{{"to", "ORDERS@hawthorn-home.example"}},
		{{"to", "Orders <orders@hawthorn-home.example>"}},
		{{"cc", "support@parcels.example"}},
		{{"from", "no-reply@parcels.example"}},
		{{"subject", "Parcel PX-9701 registered – label"}},
		{{"text", "parcel PX-9701 to Jürgen Weiß is registered"}},
		{{"html", "<b>PX-9701</b>"}},
		{{"attachment", "PX-9701-label.zpl"}},
		{{"header X-Parcel-Reference", "PX-9701"}},
	} {
		if ok, err := e.matches(rows); !ok || err != nil {
			t.Errorf("%v: %v %v (%s)", rows, ok, err, e.describe())
		}
	}
	for _, rows := range [][][2]string{
		{{"to", "support@parcels.example"}},
		{{"subject", "Parcel PX-9701 registered"}},
		{{"text", "PX-9702"}},
		{{"attachment", "PX-9701-label.pdf"}},
		{{"header X-Parcel-Reference", "PX-9702"}},
	} {
		if ok, _ := e.matches(rows); ok {
			t.Errorf("%v matched", rows)
		}
	}
	if _, err := e.matches([][2]string{{"body", "x"}}); err == nil || !strings.Contains(err.Error(), `unknown email row "body"`) {
		t.Errorf("an unknown row: %v", err)
	}
	if got := e.describe(); got != `from no-reply@parcels.example to orders@hawthorn-home.example, support@parcels.example: "Parcel PX-9701 registered – label" (attached: PX-9701-label.zpl)` {
		t.Errorf("describe: %s", got)
	}
}

func TestMailboxProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"username", "axx"}}, `the mailbox property "url" is required`},
		{[][]string{{"url", "smtp://localhost:2525"}}, "a mailbox's url is http(s):// for Mailpit, pop3(s):// or imap(s)://, not smtp://"},
		{[][]string{{"url", "imaps://imap.parcels.example"}}, "a POP3 or IMAP mailbox needs a username and a password"},
		{[][]string{{"url", "http://localhost:8025"}, {"inbox", "INBOX"}}, `unknown mailbox property "inbox" (supported: url, username, password, folder)`},
	} {
		_ = h.Fails("the shops mailbox with the following properties:", tc.want, tc.rows)
	}
	_ = h.Fails("the shops mailbox has an email where:", "No mailbox is registered in this scenario", [][]string{{"to", "a@b.example"}})
}
