package database

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"
)

// selfSignedCAPEM builds a throwaway CA so the tests exercise real PEM parsing
// rather than a fixture that could drift from what x509 actually accepts.
func selfSignedCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "devkit test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestRootCAPool(t *testing.T) {
	pool, err := rootCAPool(selfSignedCAPEM(t))
	if err != nil {
		t.Fatalf("rootCAPool on a real certificate: %v", err)
	}
	if pool == nil {
		t.Fatal("rootCAPool returned a nil pool without an error")
	}
}

// A bundle that parses to nothing must be an error, not a silent fall back to
// the system trust store: that would read as pinned and verify against anything
// the machine already trusts.
func TestRootCAPoolRejectsJunk(t *testing.T) {
	for _, bundle := range []string{
		"",
		"not a certificate",
		"-----BEGIN CERTIFICATE-----\nnot base64\n-----END CERTIFICATE-----",
	} {
		if _, err := rootCAPool(bundle); !errors.Is(err, ErrInvalidCACertificate) {
			t.Errorf("rootCAPool(%q) error = %v, want ErrInvalidCACertificate", bundle, err)
		}
	}
}

func TestValidateCACert(t *testing.T) {
	ca := selfSignedCAPEM(t)
	tests := []struct {
		name    string
		caCert  string
		sslMode string
		want    error
	}{
		{name: "no bundle is always fine", caCert: "", sslMode: "disable"},
		{name: "no bundle under verify-full", caCert: "", sslMode: "verify-full"},
		{name: "bundle with verify-full", caCert: ca, sslMode: "verify-full"},
		{name: "bundle with verify-ca", caCert: ca, sslMode: "verify-ca"},
		// require encrypts but never checks an authority, so a bundle there
		// would describe a guarantee the connection does not make.
		{name: "bundle under require", caCert: ca, sslMode: "require", want: ErrCACertificateWithoutTLS},
		{name: "bundle under disable", caCert: ca, sslMode: "disable", want: ErrCACertificateWithoutTLS},
		{name: "junk bundle under verify-full", caCert: "nope", sslMode: "verify-full", want: ErrInvalidCACertificate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCACert(tc.caCert, tc.sslMode)
			if tc.want == nil && err != nil {
				t.Fatalf("validateCACert = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("validateCACert = %v, want %v", err, tc.want)
			}
		})
	}
}

// The mysql driver has its own TLS assembly; a bundle must reach it and a mode
// that cannot verify must be refused there too.
func TestRegisterMySQLTLSCarriesTheBundle(t *testing.T) {
	ca := selfSignedCAPEM(t)

	name, err := registerMySQLTLS(ConnConfig{SSLMode: "verify-full", TLSServerName: "db.example.com", RootCAPEM: ca})
	if err != nil {
		t.Fatalf("registerMySQLTLS: %v", err)
	}
	if name == "" || name == "skip-verify" || name == "true" {
		t.Fatalf("registerMySQLTLS returned %q, want a registered config name", name)
	}
	releaseMySQLTLS(name)

	// A bundle with no server name still needs a registered config rather than
	// the driver's plain "true".
	name, err = registerMySQLTLS(ConnConfig{SSLMode: "verify-full", RootCAPEM: ca})
	if err != nil {
		t.Fatalf("registerMySQLTLS without a server name: %v", err)
	}
	if name == "true" {
		t.Fatal(`registerMySQLTLS returned "true", which drops the bundle`)
	}
	releaseMySQLTLS(name)

	if _, err := registerMySQLTLS(ConnConfig{SSLMode: "require", RootCAPEM: ca}); !errors.Is(err, ErrCACertificateWithoutTLS) {
		t.Fatalf("registerMySQLTLS under require = %v, want ErrCACertificateWithoutTLS", err)
	}
}
