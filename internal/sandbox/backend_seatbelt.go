//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
)

// SeatbeltBackend starts macOS commands inside a sandbox-exec Seatbelt profile.
type SeatbeltBackend struct{}

// NewSeatbeltBackend returns a Seatbelt backend for macOS.
func NewSeatbeltBackend() *SeatbeltBackend {
	return &SeatbeltBackend{}
}

func (SeatbeltBackend) Name() string { return "seatbelt" }

func (SeatbeltBackend) Available(context.Context) error {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ErrUnavailable
		}
		return err
	}
	return nil
}

func (SeatbeltBackend) Start(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	sandboxExec, err := exec.LookPath("sandbox-exec")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, err
	}

	p = p.withDefaults()
	profilePath, err := writeSeatbeltProfileFile(buildSeatbeltProfile(p))
	if err != nil {
		return nil, err
	}

	args := []string{"-f", profilePath, "--", c.Path}
	args = append(args, c.Args...)

	cmd := exec.CommandContext(ctx, sandboxExec, args...)
	cmd.Dir = p.EffectiveChdir(c)
	if env := seatbeltProcessEnv(p); env != nil {
		cmd.Env = env
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	proc := NewProcWithBuffers(cmd, stdout, stderr)
	if err := cmd.Start(); err != nil {
		os.Remove(profilePath)
		return nil, err
	}
	os.Remove(profilePath)
	return proc, nil
}

func writeSeatbeltProfileFile(profile string) (string, error) {
	f, err := os.CreateTemp("", "devkit-seatbelt-*.sb")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(profile); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func seatbeltProcessEnv(p Policy) []string {
	if p.Env == nil && !p.EffectivePrivateHome() {
		return nil
	}
	env := p.Env
	if p.EffectivePrivateHome() {
		env = withTMPDIRForSeatbelt(env)
	}
	if env == nil {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func withTMPDIRForSeatbelt(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{"TMPDIR": "/tmp"}
	}
	out := make(map[string]string, len(env)+1)
	for key, value := range env {
		out[key] = value
	}
	out["TMPDIR"] = "/tmp"
	return out
}
