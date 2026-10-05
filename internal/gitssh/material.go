// Package gitssh lets an SSH key that DevKit holds authenticate a git
// remote, without the key ever being written to disk.
//
// git reaches an SSH remote through the ssh program, which finds its identity
// from files in ~/.ssh and from whatever ssh-agent is running. With two GitHub
// accounts that is the problem: the agent offers every key it has, and GitHub
// accepts the first one it recognises, so a push goes out as whichever account
// happened to be first. This package serves one key from a private,
// in-process ssh-agent and points ssh at exactly that agent for one command.
//
// It is a leaf: it imports nothing from the module, so Accounts can validate a
// key with it and git can use one with it, and neither needs the other.
package gitssh

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

var (
	// ErrNotAKey is text that is not a private key ssh can read.
	ErrNotAKey = errors.New("gitssh.notAKey")
	// ErrPassphraseRequired is an encrypted key given with no passphrase.
	ErrPassphraseRequired = errors.New("gitssh.passphraseRequired")
	// ErrBadPassphrase is an encrypted key given the wrong passphrase.
	ErrBadPassphrase = errors.New("gitssh.badPassphrase")
)

// Material is what an SSH-key account holds: the private key as it was
// pasted, and the passphrase that unlocks it when it has one.
type Material struct {
	PrivateKey string `json:"key"`
	Passphrase string `json:"passphrase,omitempty"`
}

// Pack is Material as the single string an account stores as its secret.
func Pack(m Material) string {
	raw, _ := json.Marshal(struct {
		V int `json:"v"`
		Material
	}{V: 1, Material: m})
	return string(raw)
}

// Unpack reads a Pack.
func Unpack(secret string) (Material, error) {
	var packed struct {
		V int `json:"v"`
		Material
	}
	if err := json.Unmarshal([]byte(secret), &packed); err != nil || packed.V != 1 || packed.PrivateKey == "" {
		return Material{}, ErrNotAKey
	}
	return packed.Material, nil
}

// KeyInfo describes a key without revealing it.
type KeyInfo struct {
	// Type is the algorithm, as ssh names it ("ssh-ed25519", "ssh-rsa").
	Type string `json:"type"`
	// Fingerprint is SHA256:… , the form GitHub and GitLab show on their
	// settings pages, so a key can be matched with what they list.
	Fingerprint string `json:"fingerprint"`
	// PublicKey is the authorized_keys line: what is pasted into GitHub.
	PublicKey string `json:"publicKey"`
	// Encrypted says the key needs its passphrase.
	Encrypted bool `json:"encrypted"`
}

// parse reads the key, using the passphrase when it needs one.
func parse(m Material) (raw any, encrypted bool, err error) {
	pem := []byte(strings.TrimSpace(m.PrivateKey) + "\n")
	raw, err = gossh.ParseRawPrivateKey(pem)
	if err == nil {
		return raw, false, nil
	}
	var missing *gossh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, false, ErrNotAKey
	}
	if m.Passphrase == "" {
		return nil, true, ErrPassphraseRequired
	}
	raw, err = gossh.ParseRawPrivateKeyWithPassphrase(pem, []byte(m.Passphrase))
	if err != nil {
		// The library reports a wrong passphrase as a decryption failure,
		// either its own or x509's; anything else means the key is damaged.
		if errors.Is(err, x509.IncorrectPasswordError) || strings.Contains(err.Error(), "decrypt") {
			return nil, true, ErrBadPassphrase
		}
		return nil, true, ErrNotAKey
	}
	return raw, true, nil
}

// Inspect reads a key and reports what it is. An encrypted key with no or the
// wrong passphrase is an error: a key that cannot be opened now cannot
// authenticate a push later, and saying so when it is saved is the point.
func Inspect(m Material) (KeyInfo, error) {
	raw, encrypted, err := parse(m)
	if err != nil {
		return KeyInfo{Encrypted: encrypted}, err
	}
	signer, err := gossh.NewSignerFromKey(raw)
	if err != nil {
		return KeyInfo{}, ErrNotAKey
	}
	public := signer.PublicKey()
	return KeyInfo{
		Type:        public.Type(),
		Fingerprint: gossh.FingerprintSHA256(public),
		PublicKey:   strings.TrimSpace(string(gossh.MarshalAuthorizedKey(public))),
		Encrypted:   encrypted,
	}, nil
}
