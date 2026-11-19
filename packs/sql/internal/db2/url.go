package db2

import (
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
)

// ParseURL reads a Db2 URL as IBM's JDBC driver takes it,
// jdbc:db2://host:port/database:property=value;property=value; (db2:// too),
// with the user and password a feature gives beside it. A URL's own user
// and password give way to those.
//
// It reads the properties that say where the database is and how to reach
// it: currentSchema, sslConnection and sslCertLocation (a file of the
// certificates, in PEM, a TLS server's own is held to). It refuses a
// securityMechanism other than a user and a password as they are, which is
// the one this client speaks, and leaves the other properties to the driver
// they are for.
func ParseURL(raw, user, password string) (Config, error) {
	rest, ok := strings.CutPrefix(raw, "jdbc:")
	if !ok {
		rest = raw
	}
	rest, ok = cutPrefixFold(rest, "db2://")
	if !ok {
		return Config{}, fmt.Errorf("db2: %q is not a URL of a Db2 server (jdbc:db2://host:50000/database)", raw)
	}
	addr, rest, ok := strings.Cut(rest, "/")
	if !ok || addr == "" {
		return Config{}, fmt.Errorf("db2: the URL %q names no database (jdbc:db2://host:50000/database)", raw)
	}
	database, properties, _ := strings.Cut(rest, ":")
	if database == "" {
		return Config{}, fmt.Errorf("db2: the URL %q names no database (jdbc:db2://host:50000/database)", raw)
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), defaultPort)
	}
	cfg := Config{Addr: addr, Database: database, User: user, Password: password}
	for _, property := range strings.Split(properties, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(property), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "user":
			if cfg.User == "" {
				cfg.User = value
			}
		case "password":
			if cfg.Password == "" {
				cfg.Password = value
			}
		case "currentschema":
			cfg.Schema = value
		case "sslconnection":
			cfg.TLS = strings.EqualFold(value, "true")
		case "sslcertlocation":
			pem, err := os.ReadFile(value)
			if err != nil {
				return Config{}, fmt.Errorf("db2: the certificates of sslCertLocation: %w", err)
			}
			cfg.RootCAs = x509.NewCertPool()
			if !cfg.RootCAs.AppendCertsFromPEM(pem) {
				return Config{}, fmt.Errorf("db2: sslCertLocation's file %s holds no certificate in PEM", value)
			}
		case "securitymechanism":
			if value != "3" {
				return Config{}, fmt.Errorf("db2: securityMechanism=%s is not one this client speaks: it sends a user and a password as they are (3), over TLS when the URL says sslConnection=true", value)
			}
		}
	}
	return cfg, nil
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}
