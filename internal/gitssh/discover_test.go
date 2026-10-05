package gitssh

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func publicLine(t *testing.T, m Material, comment string) string {
	t.Helper()
	info, err := Inspect(m)
	if err != nil {
		t.Fatal(err)
	}
	return info.PublicKey + " " + comment + "\n"
}

// fakeSSHDir is a ~/.ssh with what a real one holds: keys, a public half, a
// config, and files that are not keys at all.
func fakeSSHDir(t *testing.T) (dir, home string, keys map[string]Material) {
	t.Helper()
	home = t.TempDir()
	// ~ in an IdentityFile means the real home; here the real home is the fake one.
	t.Setenv("HOME", home)
	dir = filepath.Join(home, ".ssh")
	keys = map[string]Material{
		"id_ed25519": newKey(t, ""),
		"id_work":    newKey(t, "work-pass"),
		"id_nopub":   newKey(t, "other-pass"),
	}
	for name, m := range keys {
		writeFile(t, filepath.Join(dir, name), m.PrivateKey, 0o600)
	}
	writeFile(t, filepath.Join(dir, "id_ed25519.pub"), publicLine(t, keys["id_ed25519"], "me@personal.example"), 0o644)
	writeFile(t, filepath.Join(dir, "id_work.pub"), publicLine(t, keys["id_work"], "dev@acme.example"), 0o644)
	writeFile(t, filepath.Join(dir, "known_hosts"), "github.com ssh-ed25519 AAAA\n", 0o644)
	writeFile(t, filepath.Join(dir, "authorized_keys"), "ssh-ed25519 AAAA x\n", 0o644)
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a key\n", 0o644)
	writeFile(t, filepath.Join(dir, "config"), "Host github-work\n  HostName github.com\n  IdentityFile ~/.ssh/id_work\n", 0o600)
	return dir, home, keys
}

func TestDiscoverListsKeysAndNothingElse(t *testing.T) {
	dir, _, keys := fakeSSHDir(t)
	found, err := Discover(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, c := range found {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "id_ed25519,id_nopub,id_work" {
		t.Fatalf("found %v", names)
	}

	byName := map[string]Candidate{}
	for _, c := range found {
		byName[c.Name] = c
	}
	personal := byName["id_ed25519"]
	personalInfo, _ := Inspect(keys["id_ed25519"])
	if personal.Fingerprint != personalInfo.Fingerprint || personal.Comment != "me@personal.example" || personal.Encrypted {
		t.Fatalf("personal = %+v", personal)
	}

	// An encrypted key with its .pub beside it can still be described.
	work := byName["id_work"]
	workInfo, _ := Inspect(keys["id_work"])
	if !work.Encrypted || work.Fingerprint != workInfo.Fingerprint || work.Comment != "dev@acme.example" {
		t.Fatalf("work = %+v", work)
	}
	// Without a .pub the OpenSSH container still carries the public half.
	nopubInfo, _ := Inspect(keys["id_nopub"])
	if nopub := byName["id_nopub"]; !nopub.Encrypted || nopub.Fingerprint != nopubInfo.Fingerprint || nopub.Comment != "" {
		t.Fatalf("nopub = %+v", nopub)
	}
}

func TestDiscoverTiesAKeyToTheAliasesThatNameIt(t *testing.T) {
	dir, _, _ := fakeSSHDir(t)
	found, _ := Discover(dir, "")
	for _, c := range found {
		switch c.Name {
		case "id_work":
			if len(c.Aliases) != 1 || c.Aliases[0].Name != "github-work" || c.Aliases[0].HostName != "github.com" {
				t.Fatalf("id_work aliases = %+v", c.Aliases)
			}
		default:
			if len(c.Aliases) != 0 {
				t.Fatalf("%s was tied to %+v", c.Name, c.Aliases)
			}
		}
	}
}

// A symlink in ~/.ssh can point anywhere; following it would make DevKit read a
// file nobody chose.
func TestDiscoverNeverFollowsASymlinkOrReadsAnOversizedFile(t *testing.T) {
	dir, home, keys := fakeSSHDir(t)
	secret := filepath.Join(home, "elsewhere", "secret_key")
	writeFile(t, secret, keys["id_ed25519"].PrivateKey, 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, "linked_key")); err != nil {
		t.Skip("no symlinks here")
	}
	writeFile(t, filepath.Join(dir, "huge"), "-----BEGIN OPENSSH PRIVATE KEY-----\n"+strings.Repeat("A", maxKeyFile+10), 0o600)
	writeFile(t, filepath.Join(dir, ".hidden_key"), keys["id_ed25519"].PrivateKey, 0o600)

	found, _ := Discover(dir, "")
	for _, c := range found {
		if c.Name == "linked_key" || c.Name == "huge" || c.Name == ".hidden_key" {
			t.Fatalf("Discover listed %s", c.Name)
		}
	}
}

func TestDiscoverOfAMissingDirectoryIsEmptyNotAnError(t *testing.T) {
	found, err := Discover(filepath.Join(t.TempDir(), "nope"), "")
	if err != nil || len(found) != 0 {
		t.Fatalf("found %v, %v", found, err)
	}
}

func TestDescribeFileFollowsTheFileTheyChoseAndSaysWhyItIsNotAKey(t *testing.T) {
	dir, _, keys := fakeSSHDir(t)
	got, err := DescribeFile(filepath.Join(dir, "id_nopub"), "other-pass")
	want, _ := Inspect(Material{PrivateKey: keys["id_nopub"].PrivateKey, Passphrase: "other-pass"})
	if err != nil || got.Fingerprint != want.Fingerprint || !got.Encrypted {
		t.Fatalf("with the passphrase = %+v, %v", got, err)
	}
	if _, err := DescribeFile(filepath.Join(dir, "id_nopub"), "wrong"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("wrong passphrase = %v", err)
	}
	if _, err := DescribeFile(filepath.Join(dir, "notes.txt"), ""); !errors.Is(err, ErrNotAKey) {
		t.Fatalf("a text file = %v", err)
	}
	if _, err := DescribeFile(dir, ""); !errors.Is(err, ErrNotAKey) {
		t.Fatalf("a directory = %v", err)
	}
	// An unreadable file's content must not appear in the error.
	if _, err := DescribeFile(filepath.Join(dir, "known_hosts"), ""); err == nil || strings.Contains(err.Error(), "ssh-ed25519") {
		t.Fatalf("a non-key file leaked into the error: %v", err)
	}
}

func TestReadAliasesReadsHostBlocksAndIncludesAndSkipsWhatItCannotKnow(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".ssh", "config")
	writeFile(t, filepath.Join(home, ".ssh", "conf.d", "work.conf"), `
Host gitlab-work gl-w
  HostName gitlab.example.test
  IdentityFile ~/.ssh/id_gl
`, 0o600)
	writeFile(t, config, `
# personal
Host github.com
  IdentityFile %d/.ssh/id_me

Host github-work
  HostName GitHub.com
  IdentityFile "~/.ssh/id work"
  IdentityFile ~/.ssh/id_work2

Host *.internal !bad
  IdentityFile ~/.ssh/id_wild

Host rel
  IdentityFile relative_key

Host tokens
  IdentityFile ~/.ssh/%h_key

Host none
  IdentityFile none

Match host something
  IdentityFile ~/.ssh/id_match

Include conf.d/*.conf

Host after
  HostName after.example.test
  IdentityFile=~/.ssh/id_after
`, 0o600)

	aliases, err := ReadAliases(config, home)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Alias{}
	for _, a := range aliases {
		got[a.Name] = a
	}
	ssh := func(name string) string { return filepath.Join(home, ".ssh", name) }

	if a := got["github.com"]; len(a.IdentityFiles) != 1 || a.IdentityFiles[0] != ssh("id_me") || a.HostName != "github.com" {
		t.Fatalf("github.com = %+v", a)
	}
	if a := got["github-work"]; a.HostName != "github.com" || len(a.IdentityFiles) != 2 || a.IdentityFiles[0] != ssh("id work") {
		t.Fatalf("github-work = %+v", a)
	}
	for _, name := range []string{"gitlab-work", "gl-w"} {
		if a := got[name]; a.HostName != "gitlab.example.test" || len(a.IdentityFiles) != 1 || a.IdentityFiles[0] != ssh("id_gl") {
			t.Fatalf("%s = %+v", name, a)
		}
	}
	if a := got["after"]; len(a.IdentityFiles) != 1 || a.IdentityFiles[0] != ssh("id_after") {
		t.Fatalf("after (a block after an Include) = %+v", a)
	}
	// Wildcards, a relative path, a token, `none` and a Match block name no alias.
	for _, name := range []string{"*.internal", "!bad", "rel", "tokens", "none", "something"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s was read as an alias: %+v", name, got[name])
		}
	}
}

func TestQuickInfoReadsAnEncryptedOpenSSHKeyWithoutItsPassphrase(t *testing.T) {
	plain, encrypted := newKey(t, ""), newKey(t, "pw")
	for _, m := range []Material{plain, encrypted} {
		want, err := Inspect(m)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := QuickInfo(m.PrivateKey)
		if !ok || got.Fingerprint != want.Fingerprint || got.PublicKey != want.PublicKey || got.Type != want.Type ||
			got.Encrypted != want.Encrypted {
			t.Fatalf("QuickInfo = %+v, %v; want %+v", got, ok, want)
		}
	}
	for _, not := range []string{"", "hello", "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----"} {
		if _, ok := QuickInfo(not); ok {
			t.Errorf("QuickInfo(%q) claimed to read it", not)
		}
	}
}
