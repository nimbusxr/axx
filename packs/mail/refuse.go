package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// refuseStep has the mailbox's Mailpit refuse the mail from or to the
// addresses a pattern matches, until the scenario ends. It needs axx's
// Mailpit image, whose chaos rules match addresses
// (extensions/mailpit-chaos).
func refuseStep(word, stage string) core.StepDef {
	return core.StepDef{
		ID: Name + ".refuse." + word, Keyword: "Given", Since: since,
		Expr: "the {word} mailbox refuses mail " + word + " {string} with code {int}",
		Doc: fmt.Sprintf("Have the mailbox's mail server refuse, with that SMTP code, every email %s the addresses the text matches, "+
			"until the scenario ends: to check what your service does when its mail is refused.\n\n"+
			"- The text is an address, or a pattern where `*` stands for any text, like `*@hawthorn-home.example`; case does not matter.\n"+
			"- The code is from 400 to 599: 4xx for a failure the sender should retry, like 451, 5xx for one it should not, like 550.\n"+
			"- It needs axx's Mailpit image, `ghcr.io/nimbusxr/axx-mailpit`: plain Mailpit refuses a share of all mail, not a scenario's own.\n"+
			"- Refuse only addresses of the scenario's own, since scenarios running side by side share the mail server.", word),
		Examples: []string{fmt.Sprintf("Given the shops mailbox refuses mail %s '*@quince-and-quill.example' with code 451", word)},
		Run: func(sc *core.Scenario, a core.Args) error {
			return refuse(sc, a.String(0), stage, secrets.Expand(sc, a.String(1)), a.Int(2))
		},
	}
}

type rule struct {
	ID        string
	Stage     string
	Match     string
	ErrorCode int
}

// added are the rules a scenario added, which go when it ends.
type added struct {
	mu    sync.Mutex
	rules []addedRule
}

type addedRule struct {
	mailbox *mailbox
	id      string
}

var scenarioRules = core.NewStateKey(Name+"/rules", func(*core.Scenario) *added { return &added{} },
	func(sc *core.Scenario, a *added) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		var errs []string
		for _, r := range a.rules {
			if err := chaosAPI(context.Background(), r.mailbox, http.MethodDelete, "/api/v1/chaos/rules/"+url.PathEscape(r.id), nil, nil); err != nil {
				errs = append(errs, err.Error())
			}
		}
		a.rules = nil
		if len(errs) > 0 {
			return fmt.Errorf("could not remove the mail server's rules: %s", strings.Join(errs, "; "))
		}
		return nil
	})

func refuse(sc *core.Scenario, name, stage, match string, code int) error {
	if code < 400 || code > 599 {
		return fmt.Errorf("an SMTP code that refuses mail is from 400 to 599, not %d", code)
	}
	m, err := mailboxes.Of(sc).Get(name)
	if err != nil {
		return fmt.Errorf("no mailbox named %q in this scenario; register it with \"the %s mailbox with the following properties:\"", name, name)
	}
	if u, err := url.Parse(m.url); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("only a Mailpit mailbox (an http:// url) can refuse mail, with axx's Mailpit image; the %s mailbox is %s", name, redact(m.url))
	}
	var r rule
	if err := chaosAPI(sc.Context(), m, http.MethodPost, "/api/v1/chaos/rules", rule{Stage: stage, Match: match, ErrorCode: code}, &r); err != nil {
		return secrets.Hide(sc, err)
	}
	st := scenarioRules.Of(sc)
	st.mu.Lock()
	st.rules = append(st.rules, addedRule{mailbox: m, id: r.ID})
	st.mu.Unlock()
	sc.Log("the %s mailbox refuses mail %s %s with code %d, until the scenario ends", name, map[string]string{"sender": "from", "recipient": "to"}[stage], match, code)
	return nil
}

// chaosAPI calls the chaos rules API of axx's Mailpit image.
func chaosAPI(ctx context.Context, m *mailbox, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.url, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.username != "" {
		req.SetBasicAuth(m.username, m.password)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("the %s mailbox's Mailpit: %w", m.name, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed:
		if method == http.MethodDelete {
			return nil // gone already
		}
		return fmt.Errorf("the %s mailbox's Mailpit has no chaos rules: use axx's Mailpit image, ghcr.io/nimbusxr/axx-mailpit, which refuses the mail of particular addresses", m.name)
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("the %s mailbox's Mailpit answered %d: %s", m.name, res.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
