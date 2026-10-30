package bridge

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"devkit/internal/accounts"
)

// Importing an SSH key reads a private key off this machine's disk, which is the
// one thing here that turns "a path" into "the bytes of a secret". So it has
// three guards beyond the capability, each for a different way it could go wrong:
//
//   - It does not exist in server mode. That bridge has no authentication, so a
//     method that reads a file by path would be a file reader for any local
//     process that can reach the port.
//   - It only reads a path this window was first shown: either listed from
//     ~/.ssh, or chosen in the native file picker. A page that has been
//     tampered with cannot name /etc/shadow or ~/.aws/credentials.
//   - It is a bridge method and nothing else. There is no MCP tool for it, so no
//     agent reaches it, whatever its sandbox allows.

var (
	errSSHImportUnavailable = errors.New("bridge.sshImportUnavailable")
	errSSHKeyNotOffered     = errors.New("bridge.sshKeyNotOffered")
)

// sshKeyPaths is the set of key files this window may read.
type sshKeyPaths struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (p *sshKeyPaths) offer(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paths == nil {
		p.paths = map[string]struct{}{}
	}
	p.paths[filepath.Clean(path)] = struct{}{}
}

func (p *sshKeyPaths) offered(path string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.paths[filepath.Clean(path)]
	return ok
}

func (s *AppService) inServerMode() bool {
	if s.serverMode != nil {
		return s.serverMode()
	}
	return false // a desktop one: the shell passes the real answer (NativeHost.IsServer)
}

// sshImportGuard runs before anything that reads a key file.
func (s *AppService) sshImportGuard(path string) error {
	if s.inServerMode() {
		return errSSHImportUnavailable
	}
	if path != "" && !s.sshKeys.offered(path) {
		return errSSHKeyNotOffered
	}
	return nil
}

// AccountsDiscoverSSHKeys lists the private keys in ~/.ssh: fingerprint, type,
// the ssh config aliases that name each, and which are already saved. Nothing
// private is in the answer.
func (s *AppService) AccountsDiscoverSSHKeys() ([]accounts.KeyFile, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:ssh-import", map[string]any{
		"action": "discover_ssh_keys",
	}, func(context.Context) (any, error) {
		if err := s.sshImportGuard(""); err != nil {
			return nil, err
		}
		files, err := s.container.Accounts.DiscoverSSHKeys()
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			s.sshKeys.offer(file.Path)
		}
		if files == nil {
			files = []accounts.KeyFile{}
		}
		return files, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]accounts.KeyFile), nil
}

// AccountsChooseSSHKeyFile opens the native file picker for a key that is not in
// ~/.ssh. Cancelling returns "" and is not an error. Whatever is chosen becomes
// readable by this window, and nothing else does.
func (s *AppService) AccountsChooseSSHKeyFile() (string, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:ssh-import", map[string]any{
		"action": "choose_ssh_key_file",
	}, func(context.Context) (any, error) {
		if err := s.sshImportGuard(""); err != nil {
			return nil, err
		}
		dialog := s.openFilesDialog
		if dialog == nil {
			dialog = defaultOpenFilesDialog
		}
		paths, err := dialog("Choose a private key", "")
		if err != nil || len(paths) == 0 {
			return "", err
		}
		s.sshKeys.offer(paths[0])
		return paths[0], nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// AccountsDescribeSSHKeyFile describes one offered key file. An encrypted key
// whose public half is not in the file needs its passphrase to be described.
func (s *AppService) AccountsDescribeSSHKeyFile(path, passphrase string) (accounts.KeyFile, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:read", "account:ssh-import", map[string]any{
		"action": "describe_ssh_key_file",
	}, func(context.Context) (any, error) {
		if err := s.sshImportGuard(path); err != nil {
			return nil, err
		}
		return s.container.Accounts.DescribeSSHKeyFile(path, passphrase)
	})
	if err != nil {
		return accounts.KeyFile{}, err
	}
	return result.(accounts.KeyFile), nil
}

// AccountsDetectSSHProvider connects to each provider that takes SSH keys and
// reports which of them know this key, and as whom. It makes real connections,
// so it runs only when the person asks.
func (s *AppService) AccountsDetectSSHProvider(path, passphrase string) ([]accounts.ProviderGuess, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:ssh-import", map[string]any{
		"action": "detect_ssh_provider",
	}, func(ctx context.Context) (any, error) {
		if err := s.sshImportGuard(path); err != nil {
			return nil, err
		}
		guesses, err := s.container.Accounts.DetectSSHProvider(ctx, path, passphrase)
		if guesses == nil {
			guesses = []accounts.ProviderGuess{}
		}
		return guesses, err
	})
	if err != nil {
		return nil, err
	}
	return result.([]accounts.ProviderGuess), nil
}

// AccountsImportSSHKey reads an offered key file into an account. The file is
// left exactly as it was. aliases are ssh config hosts to bind to the new
// account, so remotes written git@<alias>:… use it without being asked.
func (s *AppService) AccountsImportSSHKey(path, provider, label, passphrase string, syncAcrossDevices bool, aliases []string) (accounts.Account, error) {
	result, err := s.call(context.Background(), "builtin.accounts", "accounts:write", "account:ssh-import", map[string]any{
		"action":   "import_ssh_key",
		"provider": provider,
		"sync":     syncAcrossDevices,
		// The path and the label are the person's, and neither belongs in an
		// audit line.
	}, func(context.Context) (any, error) {
		if err := s.sshImportGuard(path); err != nil {
			return nil, err
		}
		account, err := s.container.Accounts.ImportSSHKey(accounts.ImportSSHKeyInput{
			Path: path, Provider: provider, Label: label, Passphrase: passphrase, Local: !syncAcrossDevices,
		})
		if err != nil {
			return accounts.Account{}, err
		}
		for _, alias := range aliases {
			// A failure to bind one alias does not undo the import; the person
			// can still be asked about the remote the first time it is used.
			_ = s.container.Accounts.ChooseForAlias(alias, account.ID)
		}
		return account, nil
	})
	if err != nil {
		return accounts.Account{}, err
	}
	return result.(accounts.Account), nil
}
