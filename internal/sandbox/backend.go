package sandbox

import "context"

// Backend starts sandboxed processes according to a policy.
type Backend interface {
	Name() string
	Available(context.Context) error // nil if usable
	Start(ctx context.Context, p Policy, c Cmd) (*Proc, error)
}
