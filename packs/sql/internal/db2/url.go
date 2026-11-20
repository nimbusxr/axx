package db2

import (
	"crypto/x509"
	"errors"
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
// certificates, in PEM, a TLS server's own is held to), securityMechanism,
// of which it speaks three: a user and a password as they are (3), and
// encrypted (7 and 9), and encryptionAlgorithm, the cipher of those two,
// which is AES (2). It refuses any other of these, and leaves the other
// properties to the driver they are for.
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
		case "encryptionalgorithm":
			switch value {
			case "2":
				// AES, which is the cipher this client encrypts with.
			case "1":
				return Config{}, errors.New("db2: encryptionAlgorithm=1 asks for DES, which axx does not encrypt a password with, as a recorded login gives its key up: leave the property out, or give it 2, for AES")
			default:
				return Config{}, fmt.Errorf("db2: encryptionAlgorithm=%s is no cipher of DRDA's: axx encrypts with AES, which is 2", value)
			}
		case "securitymechanism":
			switch value {
			case "3":
				cfg.Mechanism = secUserPassword
			case "7":
				cfg.Mechanism = secEncryptedPassword
			case "9":
				cfg.Mechanism = secEncryptedUserPassword
			default:
				return Config{}, fmt.Errorf("db2: securityMechanism=%s is not one this client speaks: it sends a user and a password as they are (3) or encrypted (7, the password, and 9, the user too)", value)
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
