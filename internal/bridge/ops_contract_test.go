package bridge

import (
	"context"
	"devkit/internal/ops"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestBridgeShimsMatchTheRegistry(t *testing.T) {
	raw, err := os.ReadFile("testdata/legacy_ops_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, name := range legacy {
		allowed[name] = true
	}
	typ := reflect.TypeFor[*AppService]()
	for _, op := range ops.All() {
		d := op.Descriptor()
		if d.Exposure == ops.Agent {
			continue
		}
		if allowed[d.Bridge] {
			t.Errorf("remove migrated %s from legacy list", d.Bridge)
		}
		allowed[d.Bridge] = true
		method, ok := typ.MethodByName(d.Bridge)
		if !ok {
			t.Errorf("missing %s", d.Bridge)
			continue
		}
		offset := 1
		if method.Type.NumIn() > 1 && method.Type.In(1) == reflect.TypeFor[context.Context]() {
			offset++
		}
		in := op.InputType()
		if method.Type.NumIn()-offset != in.NumField() {
			t.Errorf("%s argument count", d.Bridge)
			continue
		}
		for i := 0; i < in.NumField(); i++ {
			field := in.Field(i)
			if len(d.BridgeArgs) > 0 {
				var ok bool
				field, ok = in.FieldByName(d.BridgeArgs[i])
				if !ok {
					t.Fatal(d.BridgeArgs[i])
				}
			}
			if method.Type.In(i+offset) != field.Type {
				t.Errorf("%s argument %d", d.Bridge, i)
			}
		}
		out := op.OutputType()
		void := out == reflect.TypeFor[ops.Unit]()
		if (!void && (method.Type.NumOut() != 2 || method.Type.Out(0) != out)) || (void && method.Type.NumOut() != 1) {
			t.Errorf("%s return type", d.Bridge)
		}
		if method.Type.Out(method.Type.NumOut()-1) != reflect.TypeFor[error]() {
			t.Errorf("%s error return", d.Bridge)
		}
	}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if !allowed[name] {
			t.Errorf("%s is neither an op nor legacy method", name)
		}
	}
	for _, name := range legacy {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("remove missing legacy method %s", name)
		}
	}
}
