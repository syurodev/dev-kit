package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

type NetworkMode string

const (
	NetworkNone NetworkMode = "none"
	NetworkHost NetworkMode = "host"
)

var defaultSensitiveDenyPaths = []string{
	"~/.ssh",
	"~/.gnupg",
	"~/.aws",
	"~/.config/gcloud",
}

type Policy struct {
	WorkDir        string
	ReadOnlyPaths  []string
	ReadWritePaths []string // extra RW beyond WorkDir
	DenyReadPaths  []string // added on top of defaults
	// DenyWritePaths are never writable, even inside WorkDir, a
	// ReadWritePaths entry or a shared home. For the files and directories
	// that steer a vendor's own policy from inside a home DevKit hands it
	// read-write — Codex reads approval rules and config from $CODEX_HOME.
	//
	// Each one must be a clean absolute path that already exists and is not
	// a symlink; Validate refuses anything else. bwrap can only bind what
	// exists, and a path skipped there would be one the agent may create —
	// an AGENTS.override.md planted for every later session — while Seatbelt
	// denied it: the two backends must not disagree about a policy file.
	// They are leaves: no WorkDir or ReadWritePaths entry may sit inside one
	// (bwrap would bind it writable on top, Seatbelt's later deny would not).
	// Meant for paths inside a grant; on Linux one outside every grant is
	// still bound, read-only, so it is readable there.
	//
	// Both backends also stop the jail from renaming any directory above
	// one, which would otherwise move the protected path aside and let the
	// agent build a writable one in its place.
	DenyWritePaths []string
	// AllowUnixSockets are unix socket paths the process may connect to —
	// the session's identity-bound DevKit MCP endpoint. Everything else stays
	// closed (NetworkHost is IP-only).
	AllowUnixSockets []string
	// ListenLoopbackPorts are loopback ports the process may listen on — a
	// vendor's sign-in callback, which the person's browser is redirected to.
	// Seatbelt denies binding a socket otherwise (measured: codex's login
	// server failed with "Operation not permitted"). bwrap needs nothing: a
	// NetworkHost jail shares the host's loopback, and a NetworkNone one has
	// no loopback the browser could reach anyway.
	ListenLoopbackPorts []int
	// AllowKeychain lets the process read the macOS login keychain. Vendor
	// agents that authenticate with a subscription keep their OAuth token
	// there, not in a file or an env var. No-op off darwin.
	AllowKeychain           bool
	Env                     map[string]string
	Network                 NetworkMode // default none
	PrivateHome             *bool       // nil defaults to true
	DangerousAllowSensitive bool        // default false; test-only escape
}

type Cmd struct {
	Path string
	Args []string // without Path
	Dir  string   // optional; default WorkDir
}

func (p Policy) Validate() error {
	if p.WorkDir == "" {
		return ErrInvalidPolicy
	}
	if !filepath.IsAbs(p.WorkDir) {
		return ErrInvalidPolicy
	}
	if !validNetworkMode(p.Network) {
		return ErrInvalidPolicy
	}
	for _, sock := range p.AllowUnixSockets {
		if !filepath.IsAbs(sock) {
			return ErrInvalidPolicy
		}
	}
	for _, port := range p.ListenLoopbackPorts {
		if port < 1 || port > 65535 {
			return ErrInvalidPolicy
		}
	}
	if err := p.validateDenyWritePaths(); err != nil {
		return err
	}
	if err := p.validateBindPaths(); err != nil {
		return err
	}
	return nil
}

func (p Policy) ValidateCmd(c Cmd) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if c.Path == "" {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) EffectiveDenyRead() []string {
	p = p.withDefaults()
	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaultSensitiveDenyPaths)*2+len(p.DenyReadPaths))

	add := func(home, path string) {
		normalized := normalizeDenyPath(home, path)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}

	hostHome, _ := os.UserHomeDir()
	policyHome := p.policyHomeDir()
	if !p.DangerousAllowSensitive {
		for _, path := range defaultSensitiveDenyPaths {
			add(hostHome, path)
		}
		if policyHome != "" && normalizePath(policyHome) != normalizePath(hostHome) {
			for _, path := range defaultSensitiveDenyPaths {
				add(policyHome, path)
			}
		}
	}
	for _, path := range p.DenyReadPaths {
		add(policyHome, path)
	}
	return out
}

// policyHomeDir returns Policy.Env["HOME"] when set, else the host user home.
func (p Policy) policyHomeDir() string {
	if p.Env != nil && p.Env["HOME"] != "" {
		return p.Env["HOME"]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// EffectiveChdir returns c.Dir when absolute and contained in WorkDir, else WorkDir.
func (p Policy) EffectiveChdir(c Cmd) string {
	if c.Dir != "" && filepath.IsAbs(c.Dir) && pathIsContainedIn(c.Dir, p.WorkDir) {
		return c.Dir
	}
	return p.WorkDir
}

// EffectivePrivateHome reports whether the backend should hide the host home.
func (p Policy) EffectivePrivateHome() bool {
	return p.PrivateHome == nil || *p.PrivateHome
}

// sharedHome is the home a backend grants whole when PrivateHome is off, or
// "" when home is private (or cannot be named).
func (p Policy) sharedHome() string {
	if p.EffectivePrivateHome() {
		return ""
	}
	return p.policyHomeDir()
}

func (p Policy) withDefaults() Policy {
	if p.Network == "" {
		p.Network = NetworkNone
	}
	return p
}

func validNetworkMode(mode NetworkMode) bool {
	return mode == "" || mode == NetworkNone || mode == NetworkHost
}

func (p Policy) validateBindPaths() error {
	denyPaths := p.resolvedEffectiveDenyRead()
	if err := validateBindPath(p.WorkDir, denyPaths); err != nil {
		return err
	}
	for _, path := range append(append([]string{}, p.ReadWritePaths...), p.ReadOnlyPaths...) {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if !pathExists(path) {
			continue
		}
		if err := validateBindPath(path, denyPaths); err != nil {
			return err
		}
	}
	// A socket is checked against the built-in deny list only, not the
	// caller's DenyReadPaths. The caller hides a directory and hands back
	// the one socket in it the process may use — Agent Hub hides DevKit's
	// data directory and grants the session's own MCP socket inside it. That
	// is a hole the caller punched on purpose, and both backends honour it:
	// bwrap binds sockets after the deny mounts, and Seatbelt's connect right
	// is a network rule the file deny does not cover.
	builtin := p
	builtin.DenyReadPaths = nil
	builtinDeny := builtin.resolvedEffectiveDenyRead()
	for _, path := range p.AllowUnixSockets {
		if strings.TrimSpace(path) == "" || !pathExists(path) {
			continue
		}
		if err := validateBindPath(path, builtinDeny); err != nil {
			return err
		}
	}
	return nil
}

func (p Policy) validateDenyWritePaths() error {
	grants := append([]string{p.WorkDir}, p.ReadWritePaths...)
	for _, path := range p.DenyWritePaths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return ErrInvalidPolicy
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidPolicy
		}
		for _, grant := range grants {
			if grant != "" && pathIsContainedIn(normalizePath(grant), path) {
				return ErrInvalidPolicy
			}
		}
	}
	return nil
}

func validateBindPath(path string, denyPaths []string) error {
	resolved, err := resolveExistingPath(path)
	if err != nil {
		return ErrInvalidPolicy
	}
	for _, denyPath := range denyPaths {
		if pathIsContainedIn(resolved, denyPath) {
			return ErrInvalidPolicy
		}
	}
	return nil
}

func (p Policy) resolvedEffectiveDenyRead() []string {
	denyPaths := p.EffectiveDenyRead()
	out := make([]string, 0, len(denyPaths))
	seen := make(map[string]struct{})
	for _, path := range denyPaths {
		normalized := resolveDenyPath(path)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeDenyPath(home, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return normalizePath(expandHome(home, path))
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	return filepath.Clean(path)
}

func resolveDenyPath(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func resolveExistingPath(path string) (string, error) {
	path = normalizePath(path)
	if path == "" {
		return "", ErrInvalidPolicy
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func expandHome(home, path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	if home == "" {
		fallback, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		home = fallback
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// withHomeEnv sets HOME to the policy's home and TMPDIR to /tmp unless the
// caller set one. Both backends clear the environment down to Policy.Env, so
// without it a jail has no HOME at all — private or shared, every CLI that
// looks for its config under ~ would look nowhere.
func withHomeEnv(p Policy, env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+2)
	for key, value := range env {
		out[key] = value
	}
	if home := p.policyHomeDir(); home != "" {
		out["HOME"] = home
	}
	if out["TMPDIR"] == "" {
		out["TMPDIR"] = "/tmp"
	}
	return out
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathIsContainedIn(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
