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
	WorkDir                 string
	ReadOnlyPaths           []string
	ReadWritePaths          []string // extra RW beyond WorkDir
	DenyReadPaths           []string // added on top of defaults
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
	if p.DangerousAllowSensitive {
		return append([]string{}, p.DenyReadPaths...)
	}

	home := p.policyHomeDir()
	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaultSensitiveDenyPaths)+len(p.DenyReadPaths))

	add := func(path string) {
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

	for _, path := range defaultSensitiveDenyPaths {
		add(path)
	}
	for _, path := range p.DenyReadPaths {
		add(path)
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

func normalizeDenyPath(home, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return expandHome(home, path)
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

func pathIsContainedIn(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
