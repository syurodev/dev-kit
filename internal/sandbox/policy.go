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
	// AllowUnixSockets are unix socket paths the process may connect to —
	// the session's identity-bound DevKit MCP endpoint. Everything else stays
	// closed (NetworkHost is IP-only).
	AllowUnixSockets []string
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
	for _, path := range append(append(append([]string{}, p.ReadWritePaths...), p.ReadOnlyPaths...), p.AllowUnixSockets...) {
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

func withPrivateHomeEnv(p Policy, env map[string]string) map[string]string {
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
