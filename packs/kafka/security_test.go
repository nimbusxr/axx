package kafka

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	keystore "github.com/pavlo-v-chernykh/keystore-go/v4"
	"github.com/youmark/pkcs8"
	"software.sslmate.com/src/go-pkcs12"
)

type testPKI struct {
	ca, server, client       *x509.Certificate
	caKey, serverKey, cliKey *ecdsa.PrivateKey
}

func newPKI(t *testing.T, serverNames []string, serverIPs []net.IP) *testPKI {
	t.Helper()
	p := &testPKI{}
	issue := func(tmpl *x509.Certificate, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if parent == nil {
			parent, parentKey = tmpl, key
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return c, key
	}
	now := time.Now()
	p.ca, p.caKey = issue(&x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil)
	p.server, p.serverKey = issue(&x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "broker"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		DNSNames: serverNames, IPAddresses: serverIPs, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, p.ca, p.caKey)
	p.client, p.cliKey = issue(&x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "client"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, p.ca, p.caKey)
	return p
}

// serve accepts one mutual-TLS connection and reports the client's
// certificate subject.
func (p *testPKI) serve(t *testing.T) (addr string, clientCN <-chan string) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(p.ca)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{p.server.Raw}, PrivateKey: p.serverKey}},
		ClientAuth:   tls.RequireAndVerifyClientCert, ClientCAs: pool, MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			tc := c.(*tls.Conn)
			if err := tc.Handshake(); err == nil && len(tc.ConnectionState().PeerCertificates) > 0 {
				ch <- tc.ConnectionState().PeerCertificates[0].Subject.CommonName
			} else {
				ch <- ""
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String(), ch
}

func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func pemOf(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func TestTLSStores(t *testing.T) {
	pki := newPKI(t, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	dir := t.TempDir()
	resolve := func(p string) (string, error) { return filepath.Join(dir, p), nil }
	const pw = "store-pw"

	keyDER, _ := x509.MarshalPKCS8PrivateKey(pki.cliKey)
	encKey, err := pkcs8.MarshalPrivateKey(pki.cliKey, []byte("key-pw"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p12Trust, err := pkcs12.Modern.EncodeTrustStore([]*x509.Certificate{pki.ca}, pw)
	if err != nil {
		t.Fatal(err)
	}
	p12Key, err := pkcs12.Modern.Encode(pki.cliKey, pki.client, []*x509.Certificate{pki.ca}, pw)
	if err != nil {
		t.Fatal(err)
	}
	jks := func(withKey bool) []byte {
		ks := keystore.New()
		if withKey {
			if err := ks.SetPrivateKeyEntry("client", keystore.PrivateKeyEntry{
				CreationTime: time.Now(), PrivateKey: keyDER,
				CertificateChain: []keystore.Certificate{{Type: "X509", Content: pki.client.Raw}, {Type: "X509", Content: pki.ca.Raw}},
			}, []byte("key-pw")); err != nil {
				t.Fatal(err)
			}
		} else if err := ks.SetTrustedCertificateEntry("ca", keystore.TrustedCertificateEntry{
			CreationTime: time.Now(), Certificate: keystore.Certificate{Type: "X509", Content: pki.ca.Raw},
		}); err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := ks.Store(&b, []byte(pw)); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	files := map[string]string{
		"ca.pem":        writeFile(t, dir, "ca.pem", pemOf("CERTIFICATE", pki.ca.Raw)),
		"client.pem":    writeFile(t, dir, "client.pem", append(pemOf("CERTIFICATE", pki.client.Raw), pemOf("ENCRYPTED PRIVATE KEY", encKey)...)),
		"trust.p12":     writeFile(t, dir, "trust.p12", p12Trust),
		"client.p12":    writeFile(t, dir, "client.p12", p12Key),
		"trust.jks":     writeFile(t, dir, "trust.jks", jks(false)),
		"client.jks":    writeFile(t, dir, "client.jks", jks(true)),
		"garbage.store": writeFile(t, dir, "garbage.store", []byte("nope")),
	}
	cases := []struct {
		name string
		spec tlsSpec
		err  string
	}{
		{name: "PEM files", spec: tlsSpec{TruststoreType: "PEM", TruststoreLocation: files["ca.pem"], KeystoreType: "PEM", KeystoreLocation: files["client.pem"], KeyPassword: "key-pw"}},
		{name: "inline PEM", spec: tlsSpec{
			TruststoreCerts: string(pemOf("CERTIFICATE", pki.ca.Raw)),
			KeystoreChain:   string(pemOf("CERTIFICATE", pki.client.Raw)), KeystoreKey: string(pemOf("PRIVATE KEY", keyDER)),
		}},
		{name: "PKCS12", spec: tlsSpec{
			TruststoreType: "PKCS12", TruststoreLocation: files["trust.p12"], TruststorePassword: pw,
			KeystoreType: "PKCS12", KeystoreLocation: files["client.p12"], KeystorePassword: pw,
		}},
		{name: "PKCS12 declared as JKS (recognized by content)", spec: tlsSpec{
			TruststoreLocation: files["trust.p12"], TruststorePassword: pw,
			KeystoreLocation: files["client.p12"], KeystorePassword: pw,
		}},
		{name: "JKS", spec: tlsSpec{
			TruststoreLocation: files["trust.jks"], TruststorePassword: pw,
			KeystoreLocation: files["client.jks"], KeystorePassword: pw, KeyPassword: "key-pw",
		}},
		{name: "JKS truststore without password", spec: tlsSpec{
			TruststoreLocation: files["trust.jks"],
			KeystoreLocation:   files["client.jks"], KeystorePassword: pw, KeyPassword: "key-pw",
		}},
		{name: "wrong PKCS12 password", spec: tlsSpec{TruststoreLocation: files["trust.p12"], TruststorePassword: "bad"}, err: "truststore trust.p12"},
		{name: "wrong key password", spec: tlsSpec{TruststoreLocation: files["ca.pem"], KeystoreLocation: files["client.pem"], KeyPassword: "bad"}, err: "ssl.key.password"},
		{name: "not a store", spec: tlsSpec{TruststoreLocation: files["garbage.store"]}, err: "truststore garbage.store"},
		{name: "missing file", spec: tlsSpec{TruststoreLocation: "missing.jks"}, err: "ssl.truststore.location"},
		{name: "old protocol", spec: tlsSpec{Protocol: "TLSv1"}, err: "ssl.protocol TLSv1 is not supported"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := c.spec.config(resolve)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("want error %q, got %v", c.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			addr, cn := pki.serve(t)
			conn, err := tls.Dial("tcp", addr, cfg)
			if err != nil {
				t.Fatalf("handshake: %v", err)
			}
			_ = conn.Handshake()
			_ = conn.Close()
			if got := <-cn; got != "client" {
				t.Errorf("the server saw client certificate %q", got)
			}
		})
	}
}

func TestTLSEndpointIdentification(t *testing.T) {
	pki := newPKI(t, []string{"broker.internal"}, nil) // no 127.0.0.1 in the certificate
	pool := string(pemOf("CERTIFICATE", pki.ca.Raw))
	client := tlsSpec{TruststoreCerts: pool, KeystoreChain: string(pemOf("CERTIFICATE", pki.client.Raw))}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(pki.cliKey)
	client.KeystoreKey = string(pemOf("PRIVATE KEY", keyDER))

	cfg, err := client.config(nil)
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := pki.serve(t)
	if c, err := tls.Dial("tcp", addr, cfg); err == nil {
		_ = c.Close()
		t.Fatal("the default (https) must verify the host name")
	}
	empty := ""
	client.EndpointID = &empty
	cfg, err = client.config(nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("an empty ssl.endpoint.identification.algorithm skips only the host name check: %v", err)
	}
	_ = c.Close()

	other := newPKI(t, []string{"broker.internal"}, nil)
	client.TruststoreCerts = string(pemOf("CERTIFICATE", other.ca.Raw))
	cfg, _ = client.config(nil)
	if c, err := tls.Dial("tcp", addr, cfg); err == nil {
		_ = c.Close()
		t.Fatal("the chain must still be verified")
	}
}
