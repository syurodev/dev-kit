// internal/mcp/tools_codegraph_test.go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/codegraph"
)

type fakeCodeGraphAPI struct {
	endpoints []codegraph.Endpoint
	flow      codegraph.ApiFlow
	matches   []codegraph.SymbolMatch
	err       error
}

func (f fakeCodeGraphAPI) ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error) {
	return f.endpoints, f.err
}
func (f fakeCodeGraphAPI) GetApiFlow(ctx context.Context, id string) (codegraph.ApiFlow, error) {
	return f.flow, f.err
}
func (f fakeCodeGraphAPI) SearchSymbol(ctx context.Context, q string) ([]codegraph.SymbolMatch, error) {
	return f.matches, f.err
}

func TestToolCodeGraphListEndpoints(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{endpoints: []codegraph.Endpoint{{ID: "ep1", Path: "/orders"}}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphListEndpoints(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphListEndpoints: %v", err)
	}
	endpoints, ok := v.([]codegraph.Endpoint)
	if !ok || len(endpoints) != 1 || endpoints[0].Path != "/orders" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
