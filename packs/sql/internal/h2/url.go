package h2

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrPrivate says a URL names a database that only the process that opened it
// can reach: one in its memory, or a file it opened for itself.
var ErrPrivate = errors.New("the database is its service's own")

// private is an error that is ErrPrivate, in words of its own.
type private string

func (p private) Error() string        { return string(p) }
func (p private) Is(target error) bool { return target == ErrPrivate }

// ParseURL reads an H2 URL as a JDBC client writes it (jdbc:h2:..., or
// h2:...) into how to reach the database from outside the process that has
// it open:
//
//	jdbc:h2:tcp://localhost:9092/~/parcels      a database of an H2 server
//	jdbc:h2:tcp://localhost/mem:parcels         one in that server's memory
//	jdbc:h2:ssl://localhost/~/parcels           over TLS
//	jdbc:h2:file:./data/parcels;AUTO_SERVER=TRUE  a file a service opened so
//	jdbc:h2:./data/parcels;AUTO_SERVER=TRUE       that others can reach it
//
// A database in a process's memory (jdbc:h2:mem:) and a file opened without
// AUTO_SERVER are that process's only: ErrPrivate.
func ParseURL(raw, user, password string) (Config, error) {
	rest := strings.TrimSpace(raw)
	if len(rest) >= 5 && strings.EqualFold(rest[:5], "jdbc:") {
		rest = rest[5:]
	}
	if len(rest) < 3 || !strings.EqualFold(rest[:3], "h2:") {
		return Config{}, fmt.Errorf("%q is not an H2 URL (jdbc:h2:...)", raw)
	}
	name, settings, err := splitSettings(rest[3:])
	if err != nil {
		return Config{}, fmt.Errorf("H2 URL %q: %w", raw, err)
	}
	cfg := Config{User: user, Password: password, Settings: settings, TimeZone: localZone()}
	take := func(key string) (string, bool) {
		v, ok := settings[key]
		delete(settings, key)
		return v, ok
	}
	if v, ok := take("USER"); ok && cfg.User == "" {
		cfg.User = v
	}
	if v, ok := take("PASSWORD"); ok && cfg.Password == "" {
		cfg.Password = v
	}
	if v, ok := take("TIME ZONE"); ok {
		cfg.TimeZone = strings.Trim(v, "'")
	}
	autoServer, _ := take("AUTO_SERVER")
	take("AUTO_SERVER_PORT")
	if _, ok := settings["CIPHER"]; ok {
		return Config{}, fmt.Errorf("H2 URL %q: an encrypted database (CIPHER) is not one axx can open", raw)
	}
	cfg.URL = "jdbc:h2:" + name

	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "tcp://"), strings.HasPrefix(lower, "ssl://"):
		cfg.TLS = lower[0] == 's'
		addr, database, found := strings.Cut(name[len("tcp://"):], "/")
		if !found || addr == "" || database == "" {
			return Config{}, fmt.Errorf("H2 URL %q: write the server and the database, like jdbc:h2:tcp://localhost:9092/~/parcels", raw)
		}
		if strings.Contains(addr, ",") {
			return Config{}, fmt.Errorf("H2 URL %q: a cluster of servers is not one axx can open: name one of them", raw)
		}
		cfg.Addr, cfg.Database = addr, database
		return cfg, nil
	case strings.HasPrefix(lower, "mem:"):
		return Config{}, private(fmt.Sprintf("H2 URL %q: a database in a process's memory is open to that process alone", raw))
	}
	path := name
	if i := strings.Index(name, ":"); i > 1 { // a file system's name; one letter is a Windows drive
		if !strings.EqualFold(name[:i], "file") {
			return Config{}, fmt.Errorf("H2 URL %q: axx reaches an H2 server (jdbc:h2:tcp://...) or a file database opened with AUTO_SERVER=TRUE, and %s: is neither", raw, name[:i])
		}
		path = name[i+1:]
	}
	if path == "" {
		return Config{}, private(fmt.Sprintf("H2 URL %q: a database in a process's memory is open to that process alone", raw))
	}
	if !strings.EqualFold(autoServer, "true") {
		return Config{}, private(fmt.Sprintf("H2 URL %q: a file database is open to the process that opened it alone, unless its URL says ;AUTO_SERVER=TRUE", raw))
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("H2 URL %q: %w", raw, err)
		}
		path = filepath.Join(home, path[1:])
	}
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return Config{}, fmt.Errorf("H2 URL %q: %w", raw, err)
	}
	cfg.LockFile = abs + ".lock.db"
	return cfg, nil
}

// splitSettings splits what follows jdbc:h2: into the database's name and the
// settings after it (;KEY=VALUE), by their names in upper case.
func splitSettings(s string) (name string, settings map[string]string, err error) {
	parts := strings.Split(s, ";")
	settings = map[string]string{}
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "" {
			continue
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return "", nil, fmt.Errorf("its setting %q has no value (;KEY=VALUE)", p)
		}
		settings[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	return parts[0], settings, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// lock is what H2 writes in the lock file of a database opened with
// AUTO_SERVER=TRUE: where the process that has it open listens, and the key
// that names the database to it.
type lock struct {
	server string
	key    string
}

// readLock reads a database's lock file.
func readLock(path string) (lock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return lock{}, fmt.Errorf("h2: no service has the database open: there is no %s. It is there while a service has the database open with AUTO_SERVER=TRUE in its URL", path)
	}
	if err != nil {
		return lock{}, fmt.Errorf("h2: cannot read the database's lock file: %w", err)
	}
	props := properties(string(data))
	at := lock{server: props["server"], key: props["id"]}
	if at.server == "" || at.key == "" {
		return lock{}, fmt.Errorf("h2: the service that has the database open is not listening for others: %s names no server. Its URL needs AUTO_SERVER=TRUE", path)
	}
	return at, nil
}

// properties reads a Java properties file's names and values.
func properties(text string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		var key, value strings.Builder
		into, escaped := &key, false
		for _, r := range line {
			switch {
			case escaped:
				into.WriteRune(r)
				escaped = false
			case r == '\\':
				escaped = true
			case into == &key && (r == '=' || r == ':'):
				into = &value
			default:
				into.WriteRune(r)
			}
		}
		out[strings.TrimSpace(key.String())] = strings.TrimSpace(value.String())
	}
	return out
}

// localZone is the name of the machine's time zone, for a session of H2 2,
// which has the zone its client gives it, as its Java client gives its own.
func localZone() string {
	if tz := os.Getenv("TZ"); tz != "" && !strings.ContainsAny(tz, ":,") {
		if _, err := time.LoadLocation(tz); err == nil {
			return tz
		}
	}
	if name := time.Local.String(); name != "Local" && name != "" {
		return name
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(filepath.ToSlash(target), "/zoneinfo/"); ok {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	// A zone with no name to give: its offset now.
	_, offset := time.Now().Zone()
	if offset == 0 {
		return "UTC"
	}
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("%c%02d:%02d", sign, offset/3600, offset%3600/60)
}
