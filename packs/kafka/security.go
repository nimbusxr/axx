package kafka

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/youmark/pkcs8"
	"software.sslmate.com/src/go-pkcs12"
)

// parseJAAS reads the username and password of sasl.jaas.config, e.g.
//
//	org.apache.kafka.common.security.scram.ScramLoginModule required username="app" password="secret";
//
// and checks that the login module suits the mechanism.
func parseJAAS(cfg, mechanism string) (user, pass string, err error) {
	toks, err := jaasTokens(cfg)
	if err != nil {
		return "", "", err
	}
	if len(toks) < 2 {
		return "", "", errors.New(`expected "<LoginModule> required username=\"...\" password=\"...\";"`)
	}
	module, flag := toks[0].text, toks[1].text
	switch flag {
	case "required", "requisite", "sufficient", "optional":
	default:
		return "", "", fmt.Errorf("expected a control flag (required, requisite, sufficient or optional) after %s, got %q", module, flag)
	}
	short := module[strings.LastIndexByte(module, '.')+1:]
	switch {
	case short == "PlainLoginModule" && mechanism == "PLAIN":
	case short == "ScramLoginModule" && strings.HasPrefix(mechanism, "SCRAM-"):
	case short == "PlainLoginModule" || short == "ScramLoginModule":
		return "", "", fmt.Errorf("%s does not match sasl.mechanism=%s", short, mechanism)
	default:
		return "", "", fmt.Errorf("login module %s is not supported (PlainLoginModule and ScramLoginModule are)", module)
	}
	opts := map[string]string{}
	for i := 2; i < len(toks); i++ {
		t := toks[i]
		if t.text == ";" && !t.quoted {
			break
		}
		if i+2 >= len(toks) || toks[i+1].text != "=" || toks[i+1].quoted {
			return "", "", fmt.Errorf("expected key=value after %q", t.text)
		}
		opts[t.text] = toks[i+2].text
		i += 2
	}
	user, pass = opts["username"], opts["password"]
	if user == "" {
		return "", "", errors.New("the login module has no username")
	}
	return user, pass, nil
}

type jaasToken struct {
	text   string
	quoted bool
}

// jaasTokens splits a JAAS entry like Kafka's JaasConfig parser: words,
// '=' and ';', and double-quoted strings with backslash escapes.
func jaasTokens(s string) ([]jaasToken, error) {
	var out []jaasToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case unicode.IsSpace(rune(c)):
			i++
		case c == '=' || c == ';':
			out = append(out, jaasToken{text: string(c)})
			i++
		case c == '"':
			var b strings.Builder
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated quoted value")
			}
			i++
			out = append(out, jaasToken{text: b.String(), quoted: true})
		default:
			j := i
			for j < len(s) && !unicode.IsSpace(rune(s[j])) && s[j] != '=' && s[j] != ';' && s[j] != '"' {
				j++
			}
			out = append(out, jaasToken{text: s[i:j]})
			i = j
		}
	}
	return out, nil
}

// saslMechanism builds the franz-go mechanism of a SASL client.
func saslMechanism(c *clientSpec) (sasl.Mechanism, error) {
	user, pass, err := parseJAAS(c.JAAS, c.SASLMechanism)
	if err != nil {
		return nil, err
	}
	switch c.SASLMechanism {
	case "PLAIN":
		return plain.Auth{User: user, Pass: pass}.AsMechanism(), nil
	case "SCRAM-SHA-256":
		return scram.Auth{User: user, Pass: pass}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{User: user, Pass: pass}.AsSha512Mechanism(), nil
	}
	return nil, fmt.Errorf("sasl.mechanism %s is not supported", c.SASLMechanism)
}

// securityOpts returns the TLS and SASL options of a client.
func securityOpts(c *clientSpec, resolve func(string) (string, error)) ([]kgo.Opt, error) {
	var opts []kgo.Opt
	if c.Protocol == "SSL" || c.Protocol == "SASL_SSL" {
		cfg, err := c.TLS.config(resolve)
		if err != nil {
			return nil, fmt.Errorf("%s TLS: %w", c.Role, err)
		}
		opts = append(opts, kgo.DialTLSConfig(cfg))
	}
	if strings.HasPrefix(c.Protocol, "SASL_") {
		m, err := saslMechanism(c)
		if err != nil {
			return nil, fmt.Errorf("%s SASL: %w", c.Role, err)
		}
		opts = append(opts, kgo.SASL(m))
	}
	return opts, nil
}

// config builds a tls.Config from the ssl.* properties.
func (t tlsSpec) config(resolve func(string) (string, error)) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	versions := map[string]uint16{"TLSv1.2": tls.VersionTLS12, "TLSv1.3": tls.VersionTLS13}
	switch t.Protocol {
	case "", "TLS", "TLSv1.2":
	case "TLSv1.3":
		cfg.MinVersion = tls.VersionTLS13
	default:
		return nil, fmt.Errorf("ssl.protocol %s is not supported (TLSv1.2 and TLSv1.3 are)", t.Protocol)
	}
	if len(t.EnabledProtocols) > 0 {
		lo, hi := uint16(0), uint16(0)
		for _, p := range t.EnabledProtocols {
			v, ok := versions[p]
			if !ok {
				continue // older protocols are not offered
			}
			if lo == 0 || v < lo {
				lo = v
			}
			hi = max(hi, v)
		}
		if lo == 0 {
			return nil, fmt.Errorf("ssl.enabled.protocols %s has no supported protocol (TLSv1.2, TLSv1.3)", strings.Join(t.EnabledProtocols, ","))
		}
		cfg.MinVersion, cfg.MaxVersion = max(lo, cfg.MinVersion), hi
	}
	if t.TruststoreLocation != "" || t.TruststoreCerts != "" {
		pool, err := t.trustPool(resolve)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}
	if t.KeystoreLocation != "" || t.KeystoreKey != "" {
		cert, err := t.clientCert(resolve)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	if t.EndpointID != nil && *t.EndpointID == "" {
		// Verify the chain but not the host name, like Java with an empty
		// ssl.endpoint.identification.algorithm.
		roots := cfg.RootCAs
		cfg.InsecureSkipVerify = true //nolint:gosec // the chain is verified below
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter})
			return err
		}
	}
	return cfg, nil
}

func readStore(resolve func(string) (string, error), location string) ([]byte, error) {
	path, err := resolve(location)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// storeFormat recognizes a store by content, as Java's keystore loading
// does for JKS and PKCS12; declared says what the property claimed.
func storeFormat(data []byte, declared string) string {
	switch {
	case len(data) >= 4 && data[0] == 0xFE && data[1] == 0xED && data[2] == 0xFE && data[3] == 0xED:
		return "JKS"
	case bytes.Contains(data, []byte("-----BEGIN")):
		return "PEM"
	case len(data) > 0 && data[0] == 0x30:
		return "PKCS12"
	}
	return declared
}

func (t tlsSpec) trustPool(resolve func(string) (string, error)) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if t.TruststoreCerts != "" {
		if !pool.AppendCertsFromPEM([]byte(t.TruststoreCerts)) {
			return nil, errors.New("ssl.truststore.certificates holds no PEM certificate")
		}
		return pool, nil
	}
	data, err := readStore(resolve, t.TruststoreLocation)
	if err != nil {
		return nil, fmt.Errorf("ssl.truststore.location: %w", err)
	}
	var certs []*x509.Certificate
	switch storeFormat(data, t.TruststoreType) {
	case "PEM":
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("truststore %s holds no PEM certificate", t.TruststoreLocation)
		}
		return pool, nil
	case "JKS":
		ks := keystore.New()
		if err := loadJKS(ks, data, t.TruststorePassword); err != nil {
			return nil, fmt.Errorf("truststore %s: %w", t.TruststoreLocation, err)
		}
		for _, alias := range ks.Aliases() {
			if ks.IsTrustedCertificateEntry(alias) {
				e, _ := ks.GetTrustedCertificateEntry(alias)
				c, err := x509.ParseCertificate(e.Certificate.Content)
				if err != nil {
					return nil, fmt.Errorf("truststore %s entry %s: %w", t.TruststoreLocation, alias, err)
				}
				certs = append(certs, c)
			}
		}
	default: // PKCS12
		certs, err = pkcs12.DecodeTrustStore(data, t.TruststorePassword)
		if err != nil {
			_, leaf, cas, err2 := pkcs12.DecodeChain(data, t.TruststorePassword)
			if err2 != nil {
				return nil, fmt.Errorf("truststore %s: %w", t.TruststoreLocation, err)
			}
			certs = append([]*x509.Certificate{leaf}, cas...)
		}
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("truststore %s holds no trusted certificate", t.TruststoreLocation)
	}
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool, nil
}

// loadJKS reads a JKS store. Without a password Java skips the integrity
// check, and so does axx.
func loadJKS(ks keystore.KeyStore, data []byte, password string) error {
	err := ks.Load(bytes.NewReader(data), []byte(password))
	if err != nil && password == "" && strings.Contains(err.Error(), "invalid digest") {
		return nil
	}
	return err
}

func (t tlsSpec) clientCert(resolve func(string) (string, error)) (tls.Certificate, error) {
	keyPass := t.KeyPassword
	if keyPass == "" {
		keyPass = t.KeystorePassword
	}
	if t.KeystoreKey != "" {
		return pemKeyPair([]byte(t.KeystoreChain), []byte(t.KeystoreKey), keyPass)
	}
	data, err := readStore(resolve, t.KeystoreLocation)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("ssl.keystore.location: %w", err)
	}
	switch storeFormat(data, t.KeystoreType) {
	case "PEM":
		return pemKeyPair(data, data, keyPass)
	case "JKS":
		ks := keystore.New()
		if err := loadJKS(ks, data, t.KeystorePassword); err != nil {
			return tls.Certificate{}, fmt.Errorf("keystore %s: %w", t.KeystoreLocation, err)
		}
		for _, alias := range ks.Aliases() {
			if !ks.IsPrivateKeyEntry(alias) {
				continue
			}
			e, err := ks.GetPrivateKeyEntry(alias, []byte(keyPass))
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("keystore %s key %s: %w", t.KeystoreLocation, alias, err)
			}
			key, err := pkcs8.ParsePKCS8PrivateKey(e.PrivateKey)
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("keystore %s key %s: %w", t.KeystoreLocation, alias, err)
			}
			cert := tls.Certificate{PrivateKey: key}
			for _, c := range e.CertificateChain {
				cert.Certificate = append(cert.Certificate, c.Content)
			}
			return cert, nil
		}
		return tls.Certificate{}, fmt.Errorf("keystore %s holds no private key entry", t.KeystoreLocation)
	default:
		key, leaf, cas, err := pkcs12.DecodeChain(data, t.KeystorePassword)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("keystore %s: %w", t.KeystoreLocation, err)
		}
		cert := tls.Certificate{PrivateKey: key, Certificate: [][]byte{leaf.Raw}, Leaf: leaf}
		for _, c := range cas {
			cert.Certificate = append(cert.Certificate, c.Raw)
		}
		return cert, nil
	}
}

// pemKeyPair reads a PEM certificate chain and private key; the key may be
// encrypted PKCS#8 ("ENCRYPTED PRIVATE KEY") protected by password.
func pemKeyPair(chainPEM, keyPEM []byte, password string) (tls.Certificate, error) {
	var cert tls.Certificate
	for rest := chainPEM; ; {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			cert.Certificate = append(cert.Certificate, blk.Bytes)
		}
	}
	if len(cert.Certificate) == 0 {
		return cert, errors.New("keystore holds no PEM certificate")
	}
	for rest := keyPEM; ; {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		switch blk.Type {
		case "ENCRYPTED PRIVATE KEY":
			key, err := pkcs8.ParsePKCS8PrivateKey(blk.Bytes, []byte(password))
			if err != nil {
				return cert, fmt.Errorf("decrypting the private key (ssl.key.password): %w", err)
			}
			cert.PrivateKey = key
			return cert, nil
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
			pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), pem.EncodeToMemory(blk))
			if err != nil {
				return cert, err
			}
			cert.PrivateKey = pair.PrivateKey
			return cert, nil
		}
	}
	return cert, errors.New("keystore holds no PEM private key")
}

// registryTLS is the HTTP client TLS configuration for the registry, or nil.
func registryTLS(r registrySpec, resolve func(string) (string, error)) (*tls.Config, error) {
	if !r.TLS.configured() && r.TLS.EndpointID == nil {
		return nil, nil
	}
	return r.TLS.config(resolve)
}
