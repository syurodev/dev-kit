package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeKey(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(newBridgePrivateKey(t)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSSHImportDoesNotExistInServerMode(t *testing.T) {
	dir := sshHome(t)
	key := filepath.Join(dir, "id_work")
	writeKey(t, key)
	svc, _ := newBridgeWithAccounts(t)
	svc.serverMode = func() bool { return true }
	svc.openFilesDialog = func(string, string) ([]string, error) { return []string{key}, nil }
	svc.sshKeys.offer(key) // even a path that was offered earlier

	if _, err := svc.AccountsDiscoverSSHKeys(); err == nil || !strings.Contains(err.Error(), "desktop app") {
		t.Fatalf("discover in server mode = %v", err)
	}
	if _, err := svc.AccountsChooseSSHKeyFile(); err == nil {
		t.Fatal("the file picker opened in server mode")
	}
	if _, err := svc.AccountsDescribeSSHKeyFile(key, ""); err == nil {
		t.Fatal("a key was described in server mode")
	}
	if _, err := svc.AccountsDetectSSHProvider(key, ""); err == nil {
		t.Fatal("a key was probed in server mode")
	}
	if _, err := svc.AccountsImportSSHKey(key, "github", "Work", "", false, nil); err == nil {
		t.Fatal("a key was imported in server mode")
	}
}

// A page that has been tampered with cannot make the backend read a file it was
// never shown.
func TestSSHImportReadsOnlyAPathItWasOffered(t *testing.T) {
	dir := sshHome(t)
	listed := filepath.Join(dir, "id_work")
	writeKey(t, listed)
	stranger := filepath.Join(t.TempDir(), "id_other")
	writeKey(t, stranger)
	svc, _ := newBridgeWithAccounts(t)

	if _, err := svc.AccountsImportSSHKey(listed, "github", "Work", "", false, nil); err == nil || !strings.Contains(err.Error(), "Choose the key from the list") {
		t.Fatalf("a path never offered = %v", err)
	}
	if _, err := svc.AccountsDiscoverSSHKeys(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountsImportSSHKey(stranger, "github", "Other", "", false, nil); err == nil {
		t.Fatal("a path outside what was offered was read")
	}
	// Reaching it through the same directory spelled differently is the same path.
	if _, err := svc.AccountsDescribeSSHKeyFile(filepath.Join(dir, ".", "id_work"), ""); err != nil {
		t.Fatalf("an offered path spelled with a dot: %v", err)
	}
	if _, err := svc.AccountsDescribeSSHKeyFile(filepath.Join(dir, "..", ".ssh", "id_work"), ""); err != nil {
		t.Fatalf("an offered path spelled with ..: %v", err)
	}
	if _, err := svc.AccountsDescribeSSHKeyFile("/etc/passwd", ""); err == nil {
		t.Fatal("/etc/passwd was read")
	}
}

func TestImportFromTheListKeepsTheKeyLocalAndBindsTheAliases(t *testing.T) {
	dir := sshHome(t)
	key := filepath.Join(dir, "id_work")
	writeKey(t, key)
	if err := os.WriteFile(filepath.Join(dir, "config"),
		[]byte("Host github-work\n  HostName github.com\n  IdentityFile ~/.ssh/id_work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, container := newBridgeWithAccounts(t)

	files, err := svc.AccountsDiscoverSSHKeys()
	if err != nil || len(files) != 1 || len(files[0].Aliases) != 1 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	account, err := svc.AccountsImportSSHKey(files[0].Path, "github", "Work", "", false, []string{"github-work"})
	if err != nil {
		t.Fatal(err)
	}
	if !account.Local() {
		t.Fatalf("an import that was not asked to sync left the machine: %+v", account)
	}
	if bound, ok := container.Accounts.ForAlias("github-work"); !ok || bound.ID != account.ID {
		t.Fatalf("the alias was not bound: %+v, %v", bound, ok)
	}

	// The file is where it was, and a second import says where the key went.
	if _, err := os.Stat(key); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountsImportSSHKey(files[0].Path, "github", "Again", "", false, nil); err == nil ||
		!strings.Contains(err.Error(), "already saved as “Work”") {
		t.Fatalf("a second import = %v", err)
	}
}

func TestChoosingAKeyFileMakesItReadableAndCancellingDoesNot(t *testing.T) {
	sshHome(t)
	chosen := filepath.Join(t.TempDir(), "elsewhere_key")
	writeKey(t, chosen)
	svc, _ := newBridgeWithAccounts(t)

	svc.openFilesDialog = func(string, string) ([]string, error) { return nil, nil }
	if path, err := svc.AccountsChooseSSHKeyFile(); err != nil || path != "" {
		t.Fatalf("cancelled = %q, %v", path, err)
	}
	if _, err := svc.AccountsDescribeSSHKeyFile(chosen, ""); err == nil {
		t.Fatal("a cancelled picker made a path readable")
	}

	svc.openFilesDialog = func(string, string) ([]string, error) { return []string{chosen}, nil }
	path, err := svc.AccountsChooseSSHKeyFile()
	if err != nil || path != chosen {
		t.Fatalf("chosen = %q, %v", path, err)
	}
	file, err := svc.AccountsDescribeSSHKeyFile(path, "")
	if err != nil || file.Fingerprint == "" || file.Name != "elsewhere_key" {
		t.Fatalf("file = %+v, %v", file, err)
	}
	if _, err := svc.AccountsImportSSHKey(path, "gitlab", "Elsewhere", "", true, nil); err != nil {
		t.Fatal(err)
	}
}
