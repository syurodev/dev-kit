package accounts

import (
	"context"
	"errors"
	"sync"

	"devkit/internal/gitssh"
)

// ErrKeyExists is a key that is already saved for the same provider.
var ErrKeyExists = errors.New("accounts.keyExists")

// KeyExistsError says which account already holds the key, so the refusal can
// point at it rather than just saying no.
type KeyExistsError struct {
	AccountID string
	Label     string
	Provider  string
}

func (e *KeyExistsError) Error() string { return "accounts.keyExists: " + e.Label }
func (e *KeyExistsError) Is(target error) bool {
	return target == ErrKeyExists
}

// ImportedKey is an account that already holds a key found on disk.
type ImportedKey struct {
	AccountID string `json:"accountId"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
}

// KeyAlias is a Host entry in ssh config that names the key.
type KeyAlias struct {
	Name     string `json:"name"`
	HostName string `json:"hostName"`
}

// KeyFile is a private key on this machine that could be imported, described
// without being revealed.
type KeyFile struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
	Encrypted   bool   `json:"encrypted"`
	// Aliases are the hosts ~/.ssh/config names this key for.
	Aliases []KeyAlias `json:"aliases"`
	// Imported are the accounts that already hold it.
	Imported []ImportedKey `json:"imported"`
}

func (s *Service) keyFile(c gitssh.Candidate) KeyFile {
	file := KeyFile{
		Path: c.Path, Name: c.Name, Type: c.Type, Fingerprint: c.Fingerprint,
		Comment: c.Comment, Encrypted: c.Encrypted, Aliases: []KeyAlias{}, Imported: []ImportedKey{},
	}
	for _, alias := range c.Aliases {
		file.Aliases = append(file.Aliases, KeyAlias{Name: alias.Name, HostName: alias.HostName})
	}
	if c.Fingerprint != "" {
		for _, account := range s.keysWithFingerprint(c.Fingerprint) {
			file.Imported = append(file.Imported, ImportedKey{AccountID: account.ID, Provider: account.Provider, Label: account.Label})
		}
	}
	return file
}

// DiscoverSSHKeys lists the private keys in ~/.ssh with what is already saved.
func (s *Service) DiscoverSSHKeys() ([]KeyFile, error) {
	candidates, err := gitssh.Discover("", "")
	if err != nil {
		return nil, err
	}
	files := make([]KeyFile, 0, len(candidates))
	for _, candidate := range candidates {
		files = append(files, s.keyFile(candidate))
	}
	return files, nil
}

// DescribeSSHKeyFile describes one file the person chose. An encrypted key
// whose public half cannot be read needs its passphrase to be described.
func (s *Service) DescribeSSHKeyFile(path, passphrase string) (KeyFile, error) {
	candidate, err := gitssh.DescribeFile(path, passphrase)
	if err != nil {
		return KeyFile{}, mapKeyError(err)
	}
	return s.keyFile(candidate), nil
}

// mapKeyError turns what gitssh says about a key into what accounts says.
func mapKeyError(err error) error {
	switch {
	case errors.Is(err, gitssh.ErrPassphraseRequired), errors.Is(err, gitssh.ErrBadPassphrase):
		return ErrKeyPassphrase
	case errors.Is(err, gitssh.ErrNotAKey):
		return ErrKeyInvalid
	}
	return err
}

// keysWithFingerprint is the SSH-key accounts holding a key with this
// fingerprint. It reads each key's public half where the format keeps it in the
// clear, and only opens one with its saved passphrase when it has to: opening
// an encrypted key costs a deliberately slow key derivation, and a list of keys
// should not.
func (s *Service) keysWithFingerprint(fingerprint string) []Account {
	all, err := s.List()
	if err != nil {
		return nil
	}
	var held []Account
	for _, account := range all {
		if account.Kind != KindSSHKey || (account.Scoped() && !account.Local()) {
			continue
		}
		material, err := s.ResolveSSHKey(account.ID, "", "accounts")
		if err != nil {
			continue
		}
		info, ok := gitssh.QuickInfo(material.PrivateKey)
		if !ok {
			var inspectErr error
			if info, inspectErr = gitssh.Inspect(material); inspectErr != nil {
				continue
			}
		}
		if info.Fingerprint == fingerprint {
			held = append(held, account)
		}
	}
	return held
}

// ImportSSHKeyInput is a key file to read into an account.
type ImportSSHKeyInput struct {
	Path       string
	Provider   string
	Label      string
	Passphrase string
	// Local keeps the key on this machine; false syncs it with the vault.
	Local bool
}

// ImportSSHKey reads a private key off the disk into an SSH-key account.
//
// The original file is not touched, moved or deleted. A key already saved for
// the same provider is refused with the account that holds it (the same key may
// legitimately serve two providers, never one provider twice).
func (s *Service) ImportSSHKey(in ImportSSHKeyInput) (Account, error) {
	key, err := gitssh.ReadKeyFile(in.Path)
	if err != nil {
		return Account{}, mapKeyError(err)
	}
	return s.Create(CreateInput{
		Provider: in.Provider, Kind: KindSSHKey, Label: in.Label,
		Secret: key, Passphrase: in.Passphrase, Local: in.Local,
	})
}

// ProviderGuess is a provider that accepted a key, and who it said the key is.
type ProviderGuess struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Subject  string `json:"subject"`
}

// DetectSSHProvider asks each provider that takes SSH keys whether this one is
// registered with it, and who it is. It connects to those providers — which is
// why it only runs when the person asks — and reports every one that answers:
// a key may be on GitHub and GitLab both.
func (s *Service) DetectSSHProvider(ctx context.Context, path, passphrase string) ([]ProviderGuess, error) {
	key, err := gitssh.ReadKeyFile(path)
	if err != nil {
		return nil, mapKeyError(err)
	}
	material := gitssh.Material{PrivateKey: key, Passphrase: passphrase}
	if _, err := gitssh.Inspect(material); err != nil {
		return nil, mapKeyError(err)
	}
	identify := s.identifySSH
	if identify == nil {
		identify = gitssh.Identify
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		guesses []ProviderGuess
	)
	for _, provider := range s.providers.List() {
		if provider.SSHHost == "" || !provider.Accepts(KindSSHKey) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			subject, err := identify(ctx, material, provider.SSHHost)
			if err != nil {
				return
			}
			mu.Lock()
			guesses = append(guesses, ProviderGuess{Provider: provider.ID, Name: provider.Name, Subject: subject})
			mu.Unlock()
		}()
	}
	wg.Wait()
	// Providers answer in whatever order they like; the list is in the registry's.
	ordered := make([]ProviderGuess, 0, len(guesses))
	for _, provider := range s.providers.List() {
		for _, guess := range guesses {
			if guess.Provider == provider.ID {
				ordered = append(ordered, guess)
			}
		}
	}
	return ordered, nil
}

// aliasOwner is the owner under which an alias's answer is stored. A real owner
// is a first path segment and cannot be "*".
const aliasOwner = "*"

// ChooseForAlias records that remotes written with this ssh alias
// (git@github-work:…) use this key account. The alias is the person's own
// statement of which identity a remote is for — made when they wrote
// ~/.ssh/config — so it answers before the owner of the repository is asked.
func (s *Service) ChooseForAlias(alias, accountID string) error {
	if s.remoteChoices == nil {
		return nil
	}
	alias = normalizeHost(alias)
	if alias == "" {
		return ErrInvalid
	}
	account, err := s.Get(accountID)
	if err != nil {
		return err
	}
	if account.Kind != KindSSHKey {
		return ErrInvalid
	}
	return s.remoteChoices.put(alias, aliasOwner, ViaSSH, accountID)
}

// ForAlias is the key account an ssh alias was bound to, if it still exists.
func (s *Service) ForAlias(alias string) (Account, bool) {
	if s.remoteChoices == nil {
		return Account{}, false
	}
	alias = normalizeHost(alias)
	id, err := s.remoteChoices.get(alias, aliasOwner, ViaSSH)
	if err != nil || id == "" {
		return Account{}, false
	}
	account, err := s.Get(id)
	if err != nil || account.Kind != KindSSHKey {
		// The account is gone; keeping the row would ignore the same stale
		// answer on every push for ever.
		_ = s.remoteChoices.forget(alias, aliasOwner, ViaSSH)
		return Account{}, false
	}
	return account, true
}
