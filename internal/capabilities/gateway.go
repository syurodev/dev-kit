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

// Exposure is independent of domain operation declarations. The zero value
// preserves legacy calls until their modules migrate.
type Exposure uint8

const (
	ExposureBoth Exposure = iota
	ExposureUI
	ExposureAgent
)

type CallRequest struct {
	Operation  string
	Exposure   Exposure
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
	caller, _ := identity.FromContext(ctx)
	if (request.Exposure == ExposureUI && caller.Kind != identity.KindUI) ||
		(request.Exposure == ExposureAgent && caller.Kind == identity.KindUI) {
		decision = permissions.Decision{Allowed: false, Reason: fmt.Sprintf("not exposed to %s", caller.Kind)}
	}
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
		// matching on message text. The reason and what was asked for ride after
		// it: a refusal that only says "denied" cannot be told apart from a person
		// saying no, and a missing grant is a DevKit fault the caller has to be
		// able to name.
		return nil, fmt.Errorf("%w: %s (%s on %s)", ErrPermissionDenied, decision.Reason, request.Capability, request.Scope)
	}

	return request.Handler(ctx)
}
