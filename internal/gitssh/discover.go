package gitssh

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

// maxKeyFile bounds a file read as a private key. A 16384-bit RSA key is under
// 13 KB; anything past this is not one.
const maxKeyFile = 64 << 10

// maxConfigFile bounds ~/.ssh/config and what it includes.
const maxConfigFile = 1 << 20

// Candidate is a private key found on disk, described without being revealed.
// Nothing here is secret: the fingerprint and the public line are what the
// provider's own settings page shows.
type Candidate struct {
	// Path is the key file.
	Path string `json:"path"`
	// Name is the file's base name, which is how the person knows it.
	Name string `json:"name"`
	// Type is the algorithm ("ssh-ed25519"); empty when an encrypted key's
	// public half is not beside it and so cannot be read without the passphrase.
	Type string `json:"type"`
	// Fingerprint is SHA256:…; empty in the same case.
	Fingerprint string `json:"fingerprint"`
	// Comment is the .pub file's comment, usually an email or a hostname.
	Comment   string `json:"comment"`
	Encrypted bool   `json:"encrypted"`
	// Aliases are the hosts ~/.ssh/config names this key for: the way a person
	// who keeps two GitHub accounts has told ssh which is which.
	Aliases []Alias `json:"aliases"`
}

// Alias is a Host entry in ssh config that names a key file.
type Alias struct {
	// Name is what a remote URL says (git@github-work:acme/api.git).
	Name string `json:"name"`
	// HostName is the real host it stands for; Name itself when the entry has
	// no HostName.
	HostName string `json:"hostName"`
	// IdentityFiles are the key files the entry names, as absolute paths.
	IdentityFiles []string `json:"-"`
}

// Discover lists the private keys in dir (default ~/.ssh), one level deep.
//
// It reads only regular files and never follows a symlink: a link in ~/.ssh
// can point anywhere, and listing "keys" by chasing it would be a way to make
// DevKit read a file the person never meant. It does not recurse. configFile
// is the ssh config to read aliases from; empty means ~/.ssh/config.
func Discover(dir, configFile string) ([]Candidate, error) {
	home, err := os.UserHomeDir()
	if err != nil && (dir == "" || configFile == "") {
		return nil, err
	}
	if dir == "" {
		dir = filepath.Join(home, ".ssh")
	}
	if configFile == "" {
		configFile = filepath.Join(dir, "config")
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	aliases, _ := ReadAliases(configFile, home)

	var out []Candidate
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || skipName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		candidate, ok := describeFile(path)
		if !ok {
			continue
		}
		for _, alias := range aliases {
			for _, file := range alias.IdentityFiles {
				if file == path {
					candidate.Aliases = append(candidate.Aliases, alias)
					break
				}
			}
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// skipName is what ~/.ssh holds that is not a private key.
func skipName(name string) bool {
	switch {
	case strings.HasSuffix(name, ".pub"), strings.HasPrefix(name, "known_hosts"),
		strings.HasPrefix(name, "authorized_keys"), name == "config", name == "environment",
		strings.HasPrefix(name, "."):
		return true
	}
	return false
}

// DescribeFile describes one key file the person chose. Unlike Discover it
// follows a symlink — the person named this path — and says why a file is not a
// key rather than skipping it.
func DescribeFile(path string, passphrase string) (Candidate, error) {
	raw, err := readKeyFile(path)
	if err != nil {
		return Candidate{}, err
	}
	candidate := Candidate{Path: path, Name: filepath.Base(path)}
	info, err := Inspect(Material{PrivateKey: raw, Passphrase: passphrase})
	switch {
	case err == nil:
		candidate.Type, candidate.Fingerprint, candidate.Encrypted = info.Type, info.Fingerprint, info.Encrypted
		candidate.Comment = publicComment(path)
	case errors.Is(err, ErrPassphraseRequired), errors.Is(err, ErrBadPassphrase):
		candidate.Encrypted = true
		// OpenSSH's own format carries the public half in the clear; the .pub
		// file beside the key is the fallback for any other.
		if quick, ok := QuickInfo(raw); ok {
			candidate.Type, candidate.Fingerprint = quick.Type, quick.Fingerprint
			candidate.Comment = publicComment(path)
		} else {
			fillFromPublic(&candidate, path)
		}
		if errors.Is(err, ErrBadPassphrase) {
			return candidate, err
		}
	default:
		return Candidate{}, err
	}
	return candidate, nil
}

func describeFile(path string) (Candidate, bool) {
	candidate, err := DescribeFile(path, "")
	if err != nil {
		return Candidate{}, false
	}
	return candidate, true
}

// ReadKeyFile reads a private key from a file the person chose, for storing.
// It is the only place a key's bytes come off the disk.
func ReadKeyFile(path string) (string, error) { return readKeyFile(path) }

func readKeyFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxKeyFile || info.Size() == 0 {
		return "", ErrNotAKey
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(raw)
	// A private key announces itself, and refusing anything else before it is
	// parsed keeps an arbitrary file's content out of every error path.
	if !strings.HasPrefix(strings.TrimSpace(text), "-----BEGIN ") {
		return "", ErrNotAKey
	}
	return text, nil
}

// fillFromPublic reads what an encrypted key cannot say without its passphrase
// from the .pub file beside it, when there is one.
func fillFromPublic(c *Candidate, path string) {
	line, err := os.ReadFile(path + ".pub")
	if err != nil || len(line) > 8<<10 {
		return
	}
	public, comment, _, _, err := gossh.ParseAuthorizedKey(line)
	if err != nil {
		return
	}
	c.Type, c.Fingerprint, c.Comment = public.Type(), gossh.FingerprintSHA256(public), comment
}

func publicComment(path string) string {
	line, err := os.ReadFile(path + ".pub")
	if err != nil || len(line) > 8<<10 {
		return ""
	}
	if _, comment, _, _, err := gossh.ParseAuthorizedKey(line); err == nil {
		return comment
	}
	return ""
}

// ReadAliases reads the Host entries of an ssh config that name an
// IdentityFile, following Include to a small depth. It reads text only and
// runs nothing: ssh -G would also report the default key files for every host,
// which would make every alias look as if it named id_ed25519.
//
// home expands ~ and %d in an IdentityFile. An entry using any other % token
// is skipped; guessing what %h means for a key path is how a wrong key gets
// linked to an alias.
func ReadAliases(configFile, home string) ([]Alias, error) {
	var aliases []Alias
	err := readConfig(configFile, home, 0, &aliases, &configState{})
	return aliases, err
}

type configState struct {
	hosts    []string
	hostName string
	identity []string
}

func readConfig(path, home string, depth int, out *[]Alias, state *configState) error {
	if depth > 3 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > maxConfigFile {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	flush := func() {
		if len(state.identity) > 0 {
			for _, host := range state.hosts {
				hostName := state.hostName
				if hostName == "" {
					hostName = host
				}
				*out = append(*out, Alias{Name: host, HostName: strings.ToLower(hostName), IdentityFiles: append([]string(nil), state.identity...)})
			}
		}
		state.hosts, state.hostName, state.identity = nil, "", nil
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxConfigFile)
	for scanner.Scan() {
		keyword, rest := splitConfigLine(scanner.Text())
		switch strings.ToLower(keyword) {
		case "host":
			flush()
			for _, pattern := range strings.Fields(rest) {
				if !strings.ContainsAny(pattern, "*?!") {
					state.hosts = append(state.hosts, pattern)
				}
			}
		case "match":
			// A Match block's conditions are ssh's to evaluate, not ours.
			flush()
		case "hostname":
			if len(state.hosts) > 0 && state.hostName == "" {
				state.hostName = firstField(rest)
			}
		case "identityfile":
			if len(state.hosts) == 0 {
				continue
			}
			if expanded, ok := expandIdentity(firstField(rest), home); ok {
				state.identity = append(state.identity, expanded)
			}
		case "include":
			for _, pattern := range strings.Fields(rest) {
				for _, included := range includePaths(pattern, path, home) {
					flush()
					_ = readConfig(included, home, depth+1, out, state)
					// What the included file left open is its own; lines after the
					// Include in this file do not belong to it.
					flush()
				}
			}
		}
	}
	if depth == 0 {
		flush()
	}
	return scanner.Err()
}

// splitConfigLine reads "Keyword value" and "Keyword=value", dropping comments.
func splitConfigLine(line string) (keyword, rest string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
}

func firstField(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : end+1]
		}
	}
	if fields := strings.Fields(s); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func expandIdentity(value, home string) (string, bool) {
	if value == "" || value == "none" || home == "" {
		return "", false
	}
	value = strings.ReplaceAll(value, "%d", home)
	if strings.Contains(value, "%") {
		return "", false
	}
	switch {
	case value == "~":
		value = home
	case strings.HasPrefix(value, "~/"):
		value = filepath.Join(home, value[2:])
	case !filepath.IsAbs(value):
		// ssh reads a relative IdentityFile from the working directory, which is
		// not something a list of keys can know.
		return "", false
	}
	return filepath.Clean(value), true
}

// includePaths resolves an Include pattern the way ssh does for a user config:
// relative to ~/.ssh, ~ expanded, globs allowed.
func includePaths(pattern, from, home string) []string {
	switch {
	case strings.HasPrefix(pattern, "~/"):
		pattern = filepath.Join(home, pattern[2:])
	case !filepath.IsAbs(pattern):
		pattern = filepath.Join(filepath.Dir(from), pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}
