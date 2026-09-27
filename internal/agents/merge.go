package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// member is one key of a JSON object, its value kept as written.
type member struct {
	key   string
	value json.RawMessage
}

// decodeObject reads a JSON object's members in order. Anything else,
// comments included, is an error: axx rewrites only files it can read back
// exactly.
func decodeObject(data []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var out []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("not a JSON object")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		out = append(out, member{key: key, value: raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one JSON value")
	}
	return out, nil
}

func encodeObject(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		var k bytes.Buffer
		enc := json.NewEncoder(&k)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(m.key)
		b.Write(bytes.TrimSpace(k.Bytes()))
		b.WriteByte(':')
		b.Write(m.value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// mergeJSON adds the axx server under key, keeping every other member in
// its place. It reports false, and leaves data alone, when axx is there.
func mergeJSON(data []byte, key, entry string) ([]byte, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	top, err := decodeObject(data)
	if err != nil {
		return nil, false, fmt.Errorf("it is not plain JSON (%w)", err)
	}
	at := -1
	var servers []member
	for i, m := range top {
		if m.key != key {
			continue
		}
		at = i
		if string(bytes.TrimSpace(m.value)) == "null" {
			break
		}
		if servers, err = decodeObject(m.value); err != nil {
			return nil, false, fmt.Errorf("its %q is not an object", key)
		}
		if hasServer(m.value) {
			return data, false, nil
		}
	}
	servers = append(servers, member{key: "axx", value: json.RawMessage(entry)})
	if at >= 0 {
		top[at].value = encodeObject(servers)
	} else {
		top = append(top, member{key: key, value: encodeObject(servers)})
	}
	var out bytes.Buffer
	if err := json.Indent(&out, encodeObject(top), "", "  "); err != nil {
		return nil, false, err
	}
	out.WriteByte('\n')
	return out.Bytes(), true, nil
}

var (
	tomlHeader  = regexp.MustCompile(`^\s*\[\s*([^\[\]]+?)\s*\]\s*(#.*)?$`)
	tomlCommand = regexp.MustCompile(`^\s*command\s*=\s*["']([^"']*)["']`)
	tomlArgsMCP = regexp.MustCompile(`^\s*args\s*=\s*\[\s*["']mcp["']`)
	tomlServers = regexp.MustCompile(`^\s*mcp_servers\s*[=.]`)
	tomlAxxKey  = regexp.MustCompile(`^\s*["']?axx["']?\s*[=.]`)
)

type tomlScan struct {
	// configured: a [mcp_servers.axx] table, or a server that runs `axx mcp`.
	configured bool
	// inline: mcp_servers is written in a form a new table cannot join.
	inline bool
}

// scanTOML reads what matters in a Codex config.toml line by line: the
// tables of mcp_servers and whether one of them is axx.
func scanTOML(s string) tomlScan {
	var st tomlScan
	table := ""
	command, argsMCP := false, false
	flush := func() {
		if strings.HasPrefix(table, "mcp_servers.") && command && argsMCP {
			st.configured = true
		}
		command, argsMCP = false, false
	}
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[[") {
			flush()
			table = "[[" // an array of tables: none of ours
			if strings.Contains(trimmed, "mcp_servers") {
				st.inline = true
			}
			continue
		}
		if m := tomlHeader.FindStringSubmatch(line); m != nil {
			flush()
			table = normalizeTable(m[1])
			if table == "mcp_servers.axx" || strings.HasPrefix(table, "mcp_servers.axx.") {
				st.configured = true
			}
			continue
		}
		switch {
		case table == "" && tomlServers.MatchString(line):
			st.inline = true
			if strings.Contains(line, "axx") {
				st.configured = true
			}
		case table == "mcp_servers" && tomlAxxKey.MatchString(line):
			st.configured = true
		case strings.HasPrefix(table, "mcp_servers."):
			if m := tomlCommand.FindStringSubmatch(line); m != nil && isAxx(m[1]) {
				command = true
			}
			if tomlArgsMCP.MatchString(line) {
				argsMCP = true
			}
		}
	}
	flush()
	return st
}

// normalizeTable turns `mcp_servers . "axx"` into mcp_servers.axx.
func normalizeTable(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

// mergeTOML appends the [mcp_servers.axx] table. It reports false, and
// leaves data alone, when axx is there.
func mergeTOML(data []byte) ([]byte, bool, error) {
	st := scanTOML(string(data))
	if st.configured {
		return data, false, nil
	}
	if st.inline {
		return nil, false, errors.New("its mcp_servers are written inline or as an array, which a [mcp_servers.axx] table cannot join")
	}
	s := string(data)
	if strings.TrimSpace(s) == "" {
		return []byte(tomlEntry), true, nil
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s + "\n" + tomlEntry), true, nil
}
