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

	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaultSensitiveDenyPaths)+len(p.DenyReadPaths))

	add := func(path string) {
		normalized := normalizeDenyPath(path)
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

func normalizeDenyPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return expandHome(path)
}

func expandHome(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
