package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func canary(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("CANARY-private-text")
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			canary(v.Field(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		canary(v.Index(0))
	}
}
func TestRegistryContracts(t *testing.T) {
	names := map[string]Descriptor{}
	for _, op := range All() {
		d := op.Descriptor()
		if _, ok := names[d.Name]; ok {
			t.Errorf("duplicate %s", d.Name)
		}
		names[d.Name] = d
		if d.Name == "" || d.Subject == "" || d.Capability == "" || d.Grant == "" {
			t.Errorf("incomplete %s", d.Name)
		}
		if d.Exposure != Both && d.Reason == "" {
			t.Errorf("%s: exposure reason missing", d.Name)
		}
		if d.Confirm.Policy == AskOnce && d.Confirm.Reason == "" {
			t.Errorf("%s: AskOnce reason missing", d.Name)
		}
		if (d.Risk == Destructive || d.Risk == External) && d.Confirm.Policy == Never && d.Safe == "" {
			t.Errorf("%s: safety reason missing", d.Name)
		}
		for _, fill := range []bool{false, true} {
			in := reflect.New(op.InputType()).Elem()
			if fill {
				canary(in)
			}
			scope, err := op.Resolve(context.Background(), in.Interface())
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(scope)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "CANARY") {
				t.Errorf("%s leaks private input: %s", d.Name, b)
			}
		}
	}
	for _, op := range All() {
		d := op.Descriptor()
		for _, twin := range d.Twins {
			other, ok := names[twin]
			if !ok || other.Subject != d.Subject || other.Capability != d.Capability {
				t.Errorf("%s: invalid twin %s", d.Name, twin)
			}
		}
	}
}
