//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

// BwrapBackend starts Linux commands inside a bubblewrap sandbox.
type BwrapBackend struct{}

// NewBwrapBackend returns a bubblewrap backend for Linux.
func NewBwrapBackend() *BwrapBackend {
	return &BwrapBackend{}
}

func (BwrapBackend) Name() string { return "bwrap" }

func (BwrapBackend) Available(context.Context) error {
	if _, err := exec.LookPath("bwrap"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return ErrUnavailable
		}
		return err
	}
	return nil
}

func (BwrapBackend) Start(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, err
	}

	p = p.withDefaults()
	cmd := exec.CommandContext(ctx, bwrapPath, buildBwrapArgs(p, c)...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	proc := NewProcWithBuffers(cmd, stdout, stderr)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return proc, nil
}

func (BwrapBackend) StartPiped(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrUnavailable
		}
		return nil, err
	}

	p = p.withDefaults()
	cmd := exec.CommandContext(ctx, bwrapPath, buildBwrapArgs(p, c)...)
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
