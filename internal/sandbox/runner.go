package sandbox

import "context"

// Runner validates policy and delegates process start to a Backend.
type Runner struct {
	b Backend
}

// NewRunner returns a Runner that uses b to start processes.
func NewRunner(b Backend) *Runner {
	return &Runner{b: b}
}

// DefaultRunner returns the platform default runner.
func DefaultRunner() *Runner {
	return NewRunner(defaultBackend())
}

// Available reports whether the runner's backend can start processes.
func (r *Runner) Available(ctx context.Context) error {
	if r.b == nil {
		return ErrUnavailable
	}
	return r.b.Available(ctx)
}

func (r *Runner) Start(ctx context.Context, p Policy, c Cmd) (*Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	p = p.withDefaults()
	if r.b == nil {
		return nil, ErrUnavailable
	}
	return r.b.Start(ctx, p, c)
}

type unavailableBackend struct{}

func (unavailableBackend) Name() string { return "unavailable" }

func (unavailableBackend) Available(context.Context) error {
	return ErrUnsupportedPlatform
}

func (unavailableBackend) Start(context.Context, Policy, Cmd) (*Proc, error) {
	return nil, ErrUnsupportedPlatform
}
