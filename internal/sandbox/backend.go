// Package sandbox isolates untrusted agent/tool processes (bubblewrap on Linux,
// Seatbelt on Darwin).
//
// Use Start for capture-and-wait helpers (stdout/stderr buffered). Use StartPiped
// for ACP / interactive stdio transports that need live stdin/stdout pipes.
package sandbox

import "context"

// Backend starts sandboxed processes according to a policy.
type Backend interface {
	Name() string
	Available(context.Context) error // nil if usable
	Start(ctx context.Context, p Policy, c Cmd) (*Proc, error)
	// StartPiped starts a sandboxed process with stdin/stdout pipes for interactive I/O.
	StartPiped(ctx context.Context, p Policy, c Cmd) (*Proc, error)
}
