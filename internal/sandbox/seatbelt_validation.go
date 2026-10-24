package sandbox

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// profileValidationRunner runs from DevKit's host process, never through the
// SeatbeltBackend (which would recursively validate itself).
type profileValidationRunner interface {
	Run(context.Context, Policy, string) error
}
type seatbeltValidationRunner struct{}

func (seatbeltValidationRunner) Run(ctx context.Context, p Policy, profile string) error {
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-p", profile, "--", "/usr/bin/true")
	cmd.Dir = p.WorkDir
	cmd.Env = seatbeltProcessEnv(p)
	return cmd.Run()
}

// The optional rule has uniform syntax; probe it once with the first actual
// session's full policy. A rejection drops only Unix IPC, never a file deny.
type unixProfileGate struct {
	once    sync.Once
	checked atomic.Bool
	enabled atomic.Bool
	runner  profileValidationRunner
}

var sessionUnixGate = unixProfileGate{runner: seatbeltValidationRunner{}}

func (g *unixProfileGate) profile(p Policy) string {
	if p.SessionTempDir == "" {
		return buildSeatbeltProfileWithUnixSockets(p, false)
	}
	g.once.Do(func() {
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = g.runner.Run(ctx, p, buildSeatbeltProfileWithUnixSockets(p, true))
			timedOut := errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded)
			cancel()
			if !timedOut {
				break
			}
		}
		g.enabled.Store(err == nil)
		g.checked.Store(true)
		if err != nil {
			log.Printf("sandbox: session Unix socket profile check failed; using the base profile without session Unix socket rules: %v", err)
		}
	})
	return buildSeatbeltProfileWithUnixSockets(p, g.enabled.Load())
}

// SessionUnixSocketStatus is read-only runtime state for Settings. It never
// runs a probe: only the first real session launch does that.
func SessionUnixSocketStatus() string {
	if runtime.GOOS != "darwin" {
		return "Session Unix sockets use the platform sandbox."
	}
	return sessionUnixGate.status()
}
func (g *unixProfileGate) status() string {
	if !g.checked.Load() {
		return "Session Unix sockets have not been checked yet."
	}
	if !g.enabled.Load() {
		return "Session Unix sockets are unavailable on this system. Sessions can still run."
	}
	return "Session Unix sockets are available."
}

func seatbeltProcessEnv(p Policy) []string {
	if p.Env == nil && !p.EffectivePrivateHome() {
		return nil
	}
	env := withHomeEnv(p, p.Env)
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
