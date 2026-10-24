package bridge

import (
	"context"

	"devkit/internal/capabilities"
	"devkit/internal/ops"
)

// Caller is the narrow surface internal/mcp depends on (tests fake it).
type Caller interface {
	ops.Caller
	Call(
		ctx context.Context,
		subject, capability, scope string,
		metadata map[string]any,
		handler capabilities.Handler,
	) (any, error)
}

// gatewayCaller adapts AppService to Caller without putting a method on the
// bound service. Wails binds every exported method of the service by
// reflection, so an exported Call placed the capability gateway's own generic
// entrypoint on the bridge surface. Nothing could invoke it — its last
// parameter is a Go func, which is exactly what made the binding generator
// warn about internal/capabilities — but "bound and broken" is worse than
// "not bound": it is the one door every other bridge method is a narrow,
// audited version of.
type gatewayCaller struct{ app *AppService }

// NewCaller returns the capability entrypoint internal/mcp is wired with.
func NewCaller(app *AppService) Caller { return gatewayCaller{app: app} }

func (g gatewayCaller) Call(
	ctx context.Context,
	subject, capability, scope string,
	metadata map[string]any,
	handler capabilities.Handler,
) (any, error) {
	return g.app.call(ctx, subject, capability, scope, metadata, handler)
}

var _ Caller = gatewayCaller{}

func (g gatewayCaller) CallOperation(ctx context.Context, request capabilities.CallRequest) (any, error) {
	ctx = callerContext(ctx)
	result, err := g.app.container.Capabilities.Call(ctx, request)
	if err != nil {
		return nil, publicErrorFor(ctx, request.SubjectID, err)
	}
	return result, nil
}
