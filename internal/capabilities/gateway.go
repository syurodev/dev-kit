package capabilities

import (
	"context"
	"fmt"

	"changeme/internal/audit"
	"changeme/internal/permissions"
)

type Handler func(ctx context.Context) (any, error)

type CallRequest struct {
	SubjectID  string
	Capability string
	Scope      string
	Metadata   map[string]any
	Handler    Handler
}

type Gateway struct {
	policy *permissions.Policy
	sink   audit.Sink
}

func NewGateway(policy *permissions.Policy, sink audit.Sink) *Gateway {
	return &Gateway{policy: policy, sink: sink}
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
	outcome := audit.OutcomeDenied
	if decision.Allowed {
		outcome = audit.OutcomeAllowed
	}
	g.sink.Record(audit.Event{
		SubjectID:  request.SubjectID,
		Capability: request.Capability,
		Scope:      request.Scope,
		Outcome:    outcome,
		Metadata:   audit.RedactMap(request.Metadata),
	})
	if !decision.Allowed {
		return nil, fmt.Errorf("permission denied: %s", decision.Reason)
	}

	return request.Handler(ctx)
}
