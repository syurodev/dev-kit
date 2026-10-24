package ops_test

import (
	"context"
	"devkit/internal/app"
	"devkit/internal/audit"
	"devkit/internal/capabilities"
	"devkit/internal/identity"
	"devkit/internal/ops"
	"devkit/internal/permissions"
	"errors"
	"reflect"
	"testing"
)

func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("CANARY-private")
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			fill(v.Field(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0))
	}
}
func TestEveryOpIsGrantedByTheRealPolicy(t *testing.T) {
	c := app.NewContainer()
	for _, op := range ops.All() {
		d := op.Descriptor()
		for _, canary := range []bool{false, true} {
			in := reflect.New(op.InputType()).Elem()
			if canary {
				fill(in)
			}
			scope, err := op.Resolve(context.Background(), in.Interface())
			if err != nil {
				t.Fatal(err)
			}
			if got := c.Permissions.Check(permissions.Request{SubjectID: d.Subject, Capability: d.Capability, Scope: scope.Value}); !got.Allowed {
				t.Errorf("%s: %s", d.Name, got.Reason)
			}
		}
	}
}

type caller struct{ g *capabilities.Gateway }

func (c caller) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return c.g.Call(ctx, r)
}
func TestEveryUIOpIsDeniedAndAuditedForAgents(t *testing.T) {
	for _, op := range ops.All() {
		d := op.Descriptor()
		if d.Exposure != ops.UI {
			continue
		}
		sink := audit.NewMemorySink()
		rt := ops.Runtime{Caller: caller{capabilities.NewGateway(permissions.NewPolicy(ops.Grants(op)), sink)}}
		_, err := op.InvokeJSON(identity.WithCaller(context.Background(), identity.Agent("a")), rt, []byte(`{}`))
		if !errors.Is(err, capabilities.ErrPermissionDenied) {
			t.Errorf("%s: %v", d.Name, err)
		}
		events := sink.Events()
		if len(events) != 1 || events[0].Outcome != audit.OutcomeDenied {
			t.Errorf("%s: %v", d.Name, events)
		}
	}
}
