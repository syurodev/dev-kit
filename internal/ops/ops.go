// Package ops declares transport-independent operations. Authorization and audit
// remain exclusively in capabilities.Gateway, reached through Caller.
package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"devkit/internal/apperrors"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/permissions"
)

type Exposure = capabilities.Exposure

const (
	Both  = capabilities.ExposureBoth
	UI    = capabilities.ExposureUI
	Agent = capabilities.ExposureAgent
)

type Risk uint8

const (
	Read Risk = iota
	Write
	Destructive
	External
)

type ConfirmPolicy uint8

const (
	Never ConfirmPolicy = iota
	EnvTag
	AskOnce
	Always
	Custom
)

type Confirm struct {
	Policy  ConfirmPolicy
	Callers []identity.Kind
	Reason  string
	Summary func(any) string
	Kind    interaction.Kind
	Custom  func(context.Context, Deps, any) (interaction.ConfirmRequest, bool, error)
}
type Scope struct {
	Value string
	Meta  map[string]any
}
type AgentSurface struct {
	Description  string
	Shape        func(any) any
	BudgetExempt string // reason, if exempt
	PreApproved  string // reason, if pre-approved
}
type Guard func(context.Context, Deps, any) error

// Target resolves one exact target before scope resolution. Migrating a target-
// bound module supplies its resolver; notes has none.
type Target struct {
	Resolve func(context.Context, Deps, any) (context.Context, string, error)
}
type Caller interface {
	CallOperation(context.Context, capabilities.CallRequest) (any, error)
}
type Runtime struct {
	Deps       Deps
	Caller     Caller
	Hub        *interaction.Hub
	ServerMode bool
	Origin     string
	EnvBlocks  func(string) bool
}
type Op[In, Out any] struct {
	Name, Bridge               string
	Subject, Capability, Grant string
	Scope                      func(context.Context, In) (Scope, error)
	Risk                       Risk
	Confirm                    Confirm
	Exposure                   Exposure
	Reason, Desktop, Safe      string
	Action                     string
	Guards                     []Guard
	Target                     *Target
	Agent                      AgentSurface
	Twins                      []string
	Errors                     []string
	// BridgeArgs maps the positional bridge arguments to input fields. Empty
	// means the input's fields in declaration order.
	BridgeArgs []string
	Handle     func(context.Context, Deps, In) (Out, error)
}
type Descriptor struct {
	Name, Bridge, Subject, Capability, Grant string
	Risk                                     Risk
	Confirm                                  Confirm
	Exposure                                 Exposure
	Reason, Desktop, Safe                    string
	Agent                                    AgentSurface
	Twins, Errors, BridgeArgs                []string
}
type Operation interface {
	Descriptor() Descriptor
	InputType() reflect.Type
	OutputType() reflect.Type
	Resolve(context.Context, any) (Scope, error)
	InvokeJSON(context.Context, Runtime, json.RawMessage) (any, error)
}

func (o Op[I, O]) Descriptor() Descriptor {
	return Descriptor{
		Name: o.Name, Bridge: o.Bridge, Subject: o.Subject, Capability: o.Capability, Grant: o.Grant,
		Risk: o.Risk, Confirm: o.Confirm, Exposure: o.Exposure, Reason: o.Reason, Desktop: o.Desktop, Safe: o.Safe,
		Agent: o.Agent, Twins: o.Twins, Errors: o.Errors, BridgeArgs: o.BridgeArgs,
	}
}
func (o Op[I, O]) InputType() reflect.Type                            { return reflect.TypeFor[I]() }
func (o Op[I, O]) OutputType() reflect.Type                           { return reflect.TypeFor[O]() }
func (o Op[I, O]) Resolve(ctx context.Context, in any) (Scope, error) { return o.Scope(ctx, in.(I)) }
func (o Op[I, O]) InvokeJSON(ctx context.Context, rt Runtime, raw json.RawMessage) (any, error) {
	var in I
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
	}
	out, err := Invoke(ctx, rt, o, in)
	if err != nil {
		return nil, err
	}
	if reflect.TypeFor[O]() == reflect.TypeFor[Unit]() {
		return nil, nil
	}
	if o.Agent.Shape != nil {
		return o.Agent.Shape(out), nil
	}
	return out, nil
}
func Invoke[I, O any](ctx context.Context, rt Runtime, op Op[I, O], in I) (O, error) {
	var zero O
	if _, ok := identity.FromContext(ctx); !ok {
		return zero, fmt.Errorf("operation %s requires a stamped caller", op.Name)
	}
	if rt.ServerMode && op.Desktop != "" {
		return zero, apperrors.New("validation_failed", op.Desktop, false)
	}
	env := ""
	if op.Target != nil {
		var err error
		ctx, env, err = op.Target.Resolve(ctx, rt.Deps, in)
		if err != nil {
			return zero, err
		}
	}
	scope, err := op.Scope(ctx, in)
	if err != nil {
		return zero, err
	}
	action := op.Action
	if action == "" {
		_, action, _ = strings.Cut(op.Name, ".")
	}
	meta := make(map[string]any, len(scope.Meta)+2)
	for k, v := range scope.Meta {
		meta[k] = v
	}
	meta["action"] = action
	if rt.Origin != "" {
		meta["origin"] = rt.Origin
	}
	result, err := rt.Caller.CallOperation(ctx, capabilities.CallRequest{
		SubjectID: op.Subject, Capability: op.Capability, Scope: scope.Value, Metadata: meta, Operation: op.Name, Exposure: op.Exposure,
		Handler: func(ctx context.Context) (any, error) {
			for _, guard := range op.Guards {
				if err := guard(ctx, rt.Deps, in); err != nil {
					return nil, err
				}
			}
			if err := decide(ctx, rt, op.Confirm, op.Name, env, in); err != nil {
				return nil, err
			}
			return op.Handle(ctx, rt.Deps, in)
		},
	})
	if err != nil {
		return zero, err
	}
	if result == nil {
		return zero, nil
	}
	out, ok := result.(O)
	if !ok {
		return zero, fmt.Errorf("operation %s returned %T", op.Name, result)
	}
	return out, nil
}
func decide(ctx context.Context, rt Runtime, c Confirm, name, env string, in any) error {
	caller, _ := identity.FromContext(ctx)
	applies := caller.Kind == identity.KindAgent || caller.Kind == identity.KindUserTerminal
	if len(c.Callers) > 0 {
		applies = false
		for _, kind := range c.Callers {
			applies = applies || kind == caller.Kind
		}
	}
	if !applies || c.Policy == Never {
		return nil
	}
	blocks := true
	if rt.EnvBlocks != nil {
		blocks = rt.EnvBlocks(env)
	}
	req := interaction.ConfirmRequest{Tool: name, Env: env, Kind: c.Kind, EnvBlocks: blocks || c.Policy == Always}
	if c.Summary != nil {
		req.Summary = c.Summary(in)
	}
	if c.Policy == EnvTag && !blocks {
		return nil
	}
	if c.Policy == Custom {
		if c.Custom == nil {
			return fmt.Errorf("custom confirmation missing for %s", name)
		}
		var ask bool
		var err error
		req, ask, err = c.Custom(ctx, rt.Deps, in)
		if err != nil {
			return err
		}
		if !ask {
			return nil
		}
	}
	if rt.Hub == nil {
		return interaction.ErrConfirmationRequired
	}
	return rt.Hub.Confirm(ctx, req)
}
func RecordID(value string) string {
	if len(value) != 24 {
		return "invalid"
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "invalid"
		}
	}
	return value
}
func RecordScope(kind, key, id string) Scope {
	id = RecordID(id)
	return Scope{Value: kind + ":" + id, Meta: map[string]any{key: id}}
}
func FixedScope(value string) Scope { return Scope{Value: value} }
func Grants(all ...Operation) []permissions.Grant {
	var out []permissions.Grant
	seen := map[permissions.Grant]bool{}
	for _, op := range all {
		d := op.Descriptor()
		g := permissions.Grant{SubjectID: d.Subject, Capability: d.Capability, Scope: d.Grant}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}

// All grows only as modules migrate.
func All() []Operation { return Notes() }

// Unit represents a handler with no result (the bridge returns only error).
type Unit struct{}
