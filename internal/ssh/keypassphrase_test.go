package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// testKeyPEM builds a real ed25519 key, encrypted with the passphrase when one
// is given, so these tests exercise the same parser a live dial uses.
func testKeyPEM(t *testing.T, passphrase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = gossh.MarshalPrivateKey(priv, "")
	} else {
		block, err = gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(block))
}

func keyProfile(t *testing.T, svc *Service, key, passphrase string) (ProfileSummary, error) {
	t.Helper()
	return svc.CreateProfile(CreateProfileInput{
		Name: "bastion", Host: "bastion.example.com", Port: 22,
		Username: "vu.pham", AuthMethod: AuthKey, Secret: key,
		KeyPassphrase: passphrase, Env: "dev",
	})
}

func TestCreateProfileStoresKeyPassphrase(t *testing.T) {
	svc := newService(t)
	summary, err := keyProfile(t, svc, testKeyPEM(t, "s3cret"), "s3cret")
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if !summary.HasKeyPassphrase {
		t.Error("summary.HasKeyPassphrase = false, want true")
	}
	if got := svc.loadKeyPassphrase(summary.ID); got != "s3cret" {
		t.Errorf("loadKeyPassphrase = %q, want %q", got, "s3cret")
	}

	// And it survives a listing, which is what the UI reads.
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 1 || !profiles[0].HasKeyPassphrase {
		t.Errorf("ListProfiles reported HasKeyPassphrase incorrectly: %+v", profiles)
	}
}

// The whole point of validating at save time: a profile that could never
// connect must not be created.
func TestCreateProfileRejectsBadPassphrase(t *testing.T) {
	svc := newService(t)
	if _, err := keyProfile(t, svc, testKeyPEM(t, "right"), "wrong"); err == nil {
		t.Fatal("CreateProfile with the wrong passphrase succeeded, want an error")
	}
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("a rejected profile was still saved: %+v", profiles)
	}
}

func TestCreateProfileRejectsEncryptedKeyWithNoPassphrase(t *testing.T) {
	svc := newService(t)
	_, err := keyProfile(t, svc, testKeyPEM(t, "s3cret"), "")
	if !errors.Is(err, ErrKeyPassphraseRequired) {
		t.Fatalf("CreateProfile error = %v, want ErrKeyPassphraseRequired", err)
	}
}

// An unencrypted key is the common case and must not be forced to carry one.
func TestCreateProfileUnencryptedKeyNeedsNoPassphrase(t *testing.T) {
	svc := newService(t)
	summary, err := keyProfile(t, svc, testKeyPEM(t, ""), "")
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.HasKeyPassphrase {
		t.Error("summary.HasKeyPassphrase = true for a key that has none")
	}
}

// The secret must not outlive the profile it belongs to.
func TestDeleteProfileRemovesTheStoredPassphrase(t *testing.T) {
	svc := newService(t)
	summary, err := keyProfile(t, svc, testKeyPEM(t, "s3cret"), "s3cret")
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := svc.DeleteProfile(summary.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if svc.hasKeyPassphrase(summary.ID) {
		t.Error("the passphrase item outlived its profile")
	}
}

// Saving twice replaces rather than accumulates, so "which one is current"
// never becomes a question.
func TestStoreKeyPassphraseReplaces(t *testing.T) {
	svc := newService(t)
	summary, err := keyProfile(t, svc, testKeyPEM(t, "first"), "first")
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if err := svc.storeKeyPassphrase(summary.ID, "second"); err != nil {
		t.Fatalf("storeKeyPassphrase: %v", err)
	}
	if got := svc.loadKeyPassphrase(summary.ID); got != "second" {
		t.Errorf("loadKeyPassphrase = %q, want %q", got, "second")
	}
}
