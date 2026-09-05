package capabilities

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"devkit/internal/audit"
	"devkit/internal/identity"
	"devkit/internal/permissions"
)

// ErrPermissionDenied is returned when the policy denies a capability request.
// The wrapping keeps a custom reason while errors.Is still matches, and lets
// the bridge classify authorization failures without matching message text.
var ErrPermissionDenied = errors.New("permission denied")

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
	if policy == nil {
		panic("capability policy is required")
	}
	if isNilSink(sink) {
		panic("audit sink is required")
	}

	return &Gateway{policy: policy, sink: sink}
}

func isNilSink(sink audit.Sink) bool {
	if sink == nil {
		return true
	}

	value := reflect.ValueOf(sink)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
		Caller:     identity.Label(ctx),
		At:         time.Now().UTC(),
		SubjectID:  request.SubjectID,
		Capability: request.Capability,
		Scope:      request.Scope,
		Outcome:    outcome,
		Metadata:   audit.RedactMap(request.Metadata),
	})
	if !decision.Allowed {
		// The sentinel lets the bridge classify authorization failures without
		// matching on message text; the reason detail stays out of the public
		// error contract.
		return nil, fmt.Errorf("%w: %s", ErrPermissionDenied, decision.Reason)
	}

	return request.Handler(ctx)
}
