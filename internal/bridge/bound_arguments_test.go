package bridge

import (
	"reflect"
	"testing"
	"time"
)

/*
No bound method may ask the window for a time.

Wails parses every argument out of JSON BEFORE the method runs, and a
time.Time parses as RFC3339 or not at all. So a time anywhere in a
signature — including inside a struct, three fields down — makes a real
timestamp mandatory of the caller, and the window usually has no business
having one: the timestamps in this app belong to whoever writes the bytes.

This is here because DBSaveConsole took a struct with an UpdatedAt on it.
The window sent "", every single call failed inside the binding layer with

	could not parse argument #0: parsing time "" as
	"2006-01-02T15:04:05Z07:00": cannot parse "" as "2006"

and no console was ever written to disk. Nothing caught it: the failure is
upstream of the method, so no test of the method could fail, and the only
evidence was four lines in the dev log. A signature is the one place it is
visible, so a signature is where it is checked.

Returning a time is fine and common — that direction is marshalled, not
parsed. This is only about what comes in.
*/
func TestNoBoundMethodAsksTheWindowForATime(t *testing.T) {
	timeType := reflect.TypeOf(time.Time{})

	var carriesTime func(reflect.Type, int) bool
	carriesTime = func(typ reflect.Type, depth int) bool {
		// Deep enough for the shapes actually bound, and a stop for a type
		// that contains itself.
		if typ == nil || depth > 5 {
			return false
		}
		if typ == timeType {
			return true
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			return carriesTime(typ.Elem(), depth+1)
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				if carriesTime(typ.Field(i).Type, depth+1) {
					return true
				}
			}
		}
		return false
	}

	service := reflect.TypeOf(&AppService{})
	for i := 0; i < service.NumMethod(); i++ {
		method := service.Method(i)
		for arg := 1; arg < method.Type.NumIn(); arg++ {
			if carriesTime(method.Type.In(arg), 0) {
				t.Errorf(
					"%s takes a time in argument #%d (%s). Every call will fail in the "+
						"binding layer unless the window sends a real RFC3339 stamp — take "+
						"the fields it actually has, and stamp the time where it is written.",
					method.Name, arg-1, method.Type.In(arg),
				)
			}
		}
	}
}
