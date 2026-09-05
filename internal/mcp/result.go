package mcp

import "reflect"

// structuredResultKey is the field a non-object result is wrapped in.
const structuredResultKey = "result"

// structured makes a tool result safe to put in MCP's structuredContent,
// which the spec requires to be a JSON object. The go-sdk marshals whatever
// the handler returns straight into that field, so a tool answering with a
// slice (env.listTags, notes.list, ssh.listProfiles, …), a string
// (codegraph.readFile) or a number (codegraph.waitForIndexReady) produced a
// response the client rejected outright:
//
//	Invalid result for tools/call: path ["structuredContent"],
//	expected "record", received "array"
//
// Objects pass through untouched; everything else is wrapped under "result".
func structured(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Struct, reflect.Map:
		return v
	default:
		return map[string]any{structuredResultKey: v}
	}
}
