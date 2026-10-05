package gitssh

import (
	"bytes"
	"encoding/binary"
	"encoding/pem"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

const opensshMagic = "openssh-key-v1\x00"

// QuickInfo reads what a key is without its passphrase, when the key is in
// OpenSSH's own format: that container holds the public half in the clear, ahead
// of the encrypted private part. It is what lets an encrypted key be listed with
// its fingerprint — and matched against keys already saved — without a
// passphrase prompt and without the slow key derivation that opening one costs.
// ok is false for any other format (a legacy PEM RSA key has nothing readable
// until it is decrypted).
func QuickInfo(privateKey string) (info KeyInfo, ok bool) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(privateKey)))
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" || !bytes.HasPrefix(block.Bytes, []byte(opensshMagic)) {
		return KeyInfo{}, false
	}
	rest := block.Bytes[len(opensshMagic):]
	var cipher, kdf, kdfOptions string
	var ok1, ok2, ok3 bool
	cipher, rest, ok1 = readString(rest)
	kdf, rest, ok2 = readString(rest)
	kdfOptions, rest, ok3 = readString(rest)
	_ = kdfOptions
	if !ok1 || !ok2 || !ok3 || len(rest) < 4 {
		return KeyInfo{}, false
	}
	if binary.BigEndian.Uint32(rest) != 1 {
		// Several keys in one file is legal and unheard of; naming one of them
		// would be a guess.
		return KeyInfo{}, false
	}
	blob, _, ok := readString(rest[4:])
	if !ok {
		return KeyInfo{}, false
	}
	public, err := gossh.ParsePublicKey([]byte(blob))
	if err != nil {
		return KeyInfo{}, false
	}
	return KeyInfo{
		Type:        public.Type(),
		Fingerprint: gossh.FingerprintSHA256(public),
		PublicKey:   strings.TrimSpace(string(gossh.MarshalAuthorizedKey(public))),
		Encrypted:   cipher != "none" || kdf != "none",
	}, true
}

// readString reads an ssh wire string: a uint32 length and that many bytes.
func readString(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(len(b)-4) < uint64(n) {
		return "", nil, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}
