//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"errors"
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
	// The profile rides on argv (-p) instead of a temp file: sandbox-exec
	// reads the file after exec, so unlinking it right after Start raced with
	// the child opening it.
	args := []string{"-p", buildSeatbeltProfile(p), "--", c.Path}
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
		return nil, err
	}
	return proc, nil
}

func (SeatbeltBackend) StartPiped(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
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
	// See Start: the profile rides on argv, no temp file to unlink.
	args := []string{"-p", buildSeatbeltProfile(p), "--", c.Path}
	args = append(args, c.Args...)

	cmd := exec.CommandContext(ctx, sandboxExec, args...)
	cmd.Dir = p.EffectiveChdir(c)
	if env := seatbeltProcessEnv(p); env != nil {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr := &bytes.Buffer{}
	proc := NewProcPiped(cmd, stdin, stdout, stderr)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return proc, nil
}

func seatbeltProcessEnv(p Policy) []string {
	if p.Env == nil && !p.EffectivePrivateHome() {
		return nil
	}
	env := p.Env
	if p.EffectivePrivateHome() {
		env = withPrivateHomeEnv(p, env)
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
