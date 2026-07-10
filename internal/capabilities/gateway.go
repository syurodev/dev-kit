package capabilities

import (
	"context"
	"fmt"

	"changeme/internal/permissions"
)

type Handler func(ctx context.Context) (any, error)

type CallRequest struct {
	SubjectID  string
	Capability string
	Scope      string
	Handler    Handler
}

type Gateway struct {
	policy *permissions.Policy
}

func NewGateway(policy *permissions.Policy) *Gateway {
	return &Gateway{policy: policy}
}

func (g *Gateway) Call(ctx context.Context, request CallRequest) (any, error) {
	if request.Handler == nil {
		return nil, fmt.Errorf("capability handler is required")
	}

	decision := g.policy.Check(permissions.Request{
		SubjectID:  request.SubjectID,
		Capability: request.Capability,
		Scope:      request.Scope,
	})
	if !decision.Allowed {
		return nil, fmt.Errorf("permission denied: %s", decision.Reason)
	}

	return request.Handler(ctx)
}
