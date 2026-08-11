package bridge

import (
	"context"

	"devkit/internal/capabilities"
)

// Caller is the narrow surface internal/mcp depends on (tests fake it).
type Caller interface {
	Call(
		ctx context.Context,
		subject, capability, scope string,
		metadata map[string]any,
		handler capabilities.Handler,
	) (any, error)
}

var _ Caller = (*AppService)(nil)
