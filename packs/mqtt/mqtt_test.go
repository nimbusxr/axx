package mqtt

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func TestBrokerProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"username", "depot-scanners"}}, `the mqtt broker property "url" is required`},
		{[][]string{{"url", "http://localhost:1883"}}, "the mqtt broker's url is mqtt://, mqtts://, ws:// or wss://"},
		{[][]string{{"url", "mqtt://localhost"}, {"qos", "1"}}, `unknown mqtt broker property "qos" (supported: url, username, password, client id, asyncapi)`},
	} {
		_ = h.Fails("the depots mqtt broker with the following properties:", tc.want, tc.rows)
	}
}

func TestTheURLsUserIsTheDefault(t *testing.T) {
	b, err := parse("depots", &core.Table{Rows: [][]string{{"url", "mqtt://depot-scanners:scanner-pass@localhost"}}}, func(v string) string { return v })
	if err != nil || b.username != "depot-scanners" || b.password != "scanner-pass" {
		t.Errorf("%+v %v", b, err)
	}
}

func TestMessageProperties(t *testing.T) {
	p, err := message("depots/LEJ/scans", []byte(`{}`), map[string]string{
		"qos": "2", "retain": "true", "property scanner": "LEJ-HANDHELD-7", "content type": "application/json",
		"response topic": "depots/LEJ/acks", "correlation data": "SC-8201-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	pp := p.Properties
	if p.QoS != 2 || !p.Retain || pp.User.Get("scanner") != "LEJ-HANDHELD-7" || pp.ContentType != "application/json" ||
		pp.ResponseTopic != "depots/LEJ/acks" || string(pp.CorrelationData) != "SC-8201-1" {
		t.Errorf("%+v %+v", p, pp)
	}
	if p, _ := message("depots/LEJ/scans", nil, nil); p.QoS != 1 || p.Retain {
		t.Errorf("defaults: %+v", p)
	}
	for fields, want := range map[string]map[string]string{
		`the qos is 0, 1 or 2, not "3"`:                {"qos": "3"},
		`retain is true or false, not "yes"`:           {"retain": "yes"},
		`unknown mqtt message property "header depot"`: {"header depot": "LEJ"},
	} {
		if _, err := message("depots/LEJ/scans", nil, want); err == nil || !strings.Contains(err.Error(), fields) {
			t.Errorf("%v: %v", want, err)
		}
	}
	if _, err := message("depots/+/scans", nil, nil); err == nil || !strings.Contains(err.Error(), "not a filter") {
		t.Errorf("a filter: %v", err)
	}
}

// A packet written in parts goes out whole, one packet at a time.
func TestPacketsGoOutWhole(t *testing.T) {
	var sent [][]byte
	c := &packetConn{Conn: recorder{&sent}}
	subscribe := []byte{0x82, 0x0a, 0x00, 0x01, 0x00, 0x00, 0x04, 'd', '/', '+', '/', 0x01}
	pingreq := []byte{0xc0, 0x00}
	for _, part := range [][]byte{subscribe[:1], subscribe[1:4], {}, subscribe[4:], pingreq} {
		if _, err := c.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) != 2 || string(sent[0]) != string(subscribe) || string(sent[1]) != string(pingreq) {
		t.Errorf("sent %x", sent)
	}
	// A remaining length of two bytes: 200 = 0xc8 0x01.
	if n, ok := packetLen(append([]byte{0x30, 0xc8, 0x01}, make([]byte, 200)...)); !ok || n != 203 {
		t.Errorf("packetLen: %d %v", n, ok)
	}
}

type recorder struct{ sent *[][]byte }

func (r recorder) Write(p []byte) (int, error) {
	*r.sent = append(*r.sent, append([]byte(nil), p...))
	return len(p), nil
}

func (recorder) Read([]byte) (int, error)         { return 0, nil }
func (recorder) Close() error                     { return nil }
func (recorder) LocalAddr() net.Addr              { return nil }
func (recorder) RemoteAddr() net.Addr             { return nil }
func (recorder) SetDeadline(time.Time) error      { return nil }
func (recorder) SetReadDeadline(time.Time) error  { return nil }
func (recorder) SetWriteDeadline(time.Time) error { return nil }
