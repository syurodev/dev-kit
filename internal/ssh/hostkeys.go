package ssh

import (
	"errors"
	"fmt"
	"net"

	gossh "golang.org/x/crypto/ssh"
)

var errHostKeyObserved = errors.New("ssh host key observed")

// pinnedHostKeyCallback accepts only the explicitly trusted SHA-256 host-key
// fingerprint. An empty pin is never a valid trust state.
func pinnedHostKeyCallback(fingerprint string) gossh.HostKeyCallback {
	return func(_ string, _ net.Addr, key gossh.PublicKey) error {
		if fingerprint == "" {
			return errors.New("ssh host key is not trusted")
		}
		if actual := gossh.FingerprintSHA256(key); actual != fingerprint {
			return fmt.Errorf("ssh host key fingerprint mismatch")
		}
		return nil
	}
}

// observeHostKeyCallback records the key supplied during SSH handshake and
// stops the connection before authentication proceeds.
func observeHostKeyCallback(observed *string) gossh.HostKeyCallback {
	return func(_ string, _ net.Addr, key gossh.PublicKey) error {
		*observed = gossh.FingerprintSHA256(key)
		return errHostKeyObserved
	}
}

func parsePrivateKey(privateKeyPEM []byte, passphrase string) (gossh.Signer, error) {
	signer, err := gossh.ParsePrivateKey(privateKeyPEM)
	if err == nil {
		return signer, nil
	}
	var missingPassphrase *gossh.PassphraseMissingError
	if !errors.As(err, &missingPassphrase) {
		return nil, err
	}

	passphraseBytes := []byte(passphrase)
	defer zeroBytes(passphraseBytes)
	return gossh.ParsePrivateKeyWithPassphrase(privateKeyPEM, passphraseBytes)
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
