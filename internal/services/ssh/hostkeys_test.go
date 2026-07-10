package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestPinnedHostKeyCallbackRejectsUnknownHost(t *testing.T) {
	_, publicKey := newHostKey(t)

	err := pinnedHostKeyCallback("")("example.test:22", nil, publicKey)
	if err == nil {
		t.Fatal("empty host-key fingerprint must reject the connection")
	}
}

func TestPinnedHostKeyCallbackRejectsChangedHostKey(t *testing.T) {
	_, trustedKey := newHostKey(t)
	_, presentedKey := newHostKey(t)

	err := pinnedHostKeyCallback(gossh.FingerprintSHA256(trustedKey))("example.test:22", nil, presentedKey)
	if err == nil {
		t.Fatal("changed host key must reject the connection")
	}
}

func TestPinnedHostKeyCallbackAcceptsExactFingerprint(t *testing.T) {
	_, publicKey := newHostKey(t)

	err := pinnedHostKeyCallback(gossh.FingerprintSHA256(publicKey))("example.test:22", nil, publicKey)
	if err != nil {
		t.Fatalf("exact fingerprint rejected: %v", err)
	}
}

func TestParsePrivateKeyRequiresCorrectPassphraseForEncryptedPEM(t *testing.T) {
	privateKey, _ := newHostKey(t)
	block, err := gossh.MarshalPrivateKeyWithPassphrase(privateKey, "", []byte("correct horse battery staple"))
	if err != nil {
		t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
	}
	pem := pem.EncodeToMemory(block)

	if _, err := parsePrivateKey(pem, ""); err == nil {
		t.Fatal("encrypted PEM without a passphrase must be rejected")
	}
	if _, err := parsePrivateKey(pem, "wrong passphrase"); err == nil {
		t.Fatal("encrypted PEM with a wrong passphrase must be rejected")
	}
	if _, err := parsePrivateKey(pem, "correct horse battery staple"); err != nil {
		t.Fatalf("encrypted PEM with correct passphrase rejected: %v", err)
	}
}

func newHostKey(t *testing.T) (*rsa.PrivateKey, gossh.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	publicKey, err := gossh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	return privateKey, publicKey
}
