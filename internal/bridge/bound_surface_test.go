package bridge

import (
	"reflect"
	"testing"

	"devkit/internal/app"
)

// Wails binds every exported method of the bound service by reflection, and a
// Go func can never cross the JSON boundary. An exported method carrying one
// is therefore dead on the wire, and the binding generator says so:
//
//	package devkit/internal/capabilities: function types are not supported by
//	encoding/json
//
// That warning was AppService.Call — the capability gateway's generic
// entrypoint, bound to the frontend by accident. Unexporting it is the fix;
// this is the guard that notices the next one.
func TestBoundSurfaceCarriesNoFuncs(t *testing.T) {
	svc := reflect.TypeOf(NewAppService(app.NewContainer()))

	for i := range svc.NumMethod() {
		method := svc.Method(i)
		signature := method.Type
		for in := range signature.NumIn() {
			if signature.In(in).Kind() == reflect.Func {
				t.Errorf("%s takes a func in position %d; Wails binds it and JSON cannot carry it", method.Name, in)
			}
		}
		for out := range signature.NumOut() {
			if signature.Out(out).Kind() == reflect.Func {
				t.Errorf("%s returns a func in position %d; Wails binds it and JSON cannot carry it", method.Name, out)
			}
		}
	}
}
