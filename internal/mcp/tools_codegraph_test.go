// internal/mcp/tools_codegraph_test.go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/codegraph"
)

type fakeCodeGraphAPI struct {
	endpoints     []codegraph.Endpoint
	flow          codegraph.ApiFlow
	matches       []codegraph.SymbolMatch
	files         []codegraph.FileEntry
	fileContent   string
	snippet       codegraph.CodeSnippet
	blastRadius   []string
	indexVersion  int
	subgraph      codegraph.Subgraph
	dataFlowTrace codegraph.DataFlowTrace
	err           error
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
func (f fakeCodeGraphAPI) ListFiles(ctx context.Context) ([]codegraph.FileEntry, error) {
	return f.files, f.err
}
func (f fakeCodeGraphAPI) ReadFile(ctx context.Context, relativePath string) (string, error) {
	return f.fileContent, f.err
}
func (f fakeCodeGraphAPI) GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (codegraph.CodeSnippet, error) {
	return f.snippet, f.err
}
func (f fakeCodeGraphAPI) GetBlastRadius(ctx context.Context, filePath string) ([]string, error) {
	return f.blastRadius, f.err
}
func (f fakeCodeGraphAPI) WaitForIndexReady(ctx context.Context) (int, error) {
	return f.indexVersion, f.err
}
func (f fakeCodeGraphAPI) GetNeighbors(ctx context.Context, rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error) {
	return f.subgraph, f.err
}
func (f fakeCodeGraphAPI) TraceDataFlow(ctx context.Context, symbolID, fieldName string) (codegraph.DataFlowTrace, error) {
	return f.dataFlowTrace, f.err
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

func TestToolCodeGraphReadFile(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{fileContent: "package com.acme;"},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphReadFile(context.Background(), nil, codeGraphReadFileInput{RelativePath: "Foo.java"})
	if err != nil {
		t.Fatalf("toolCodeGraphReadFile: %v", err)
	}
	content, ok := v.(string)
	if !ok || content != "package com.acme;" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphGetBlastRadius(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{blastRadius: []string{"Controller.java"}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetBlastRadius(context.Background(), nil, codeGraphGetBlastRadiusInput{FilePath: "Service.java"})
	if err != nil {
		t.Fatalf("toolCodeGraphGetBlastRadius: %v", err)
	}
	radius, ok := v.([]string)
	if !ok || len(radius) != 1 || radius[0] != "Controller.java" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphWaitForIndexReady(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{indexVersion: 3},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphWaitForIndexReady(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphWaitForIndexReady: %v", err)
	}
	version, ok := v.(int)
	if !ok || version != 3 {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphGetNeighbors(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{subgraph: codegraph.Subgraph{
			Nodes: []codegraph.Node{{ID: "n1", Label: "A"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetNeighbors(context.Background(), nil, codeGraphGetNeighborsInput{RootID: "n1", Depth: 1})
	if err != nil {
		t.Fatalf("toolCodeGraphGetNeighbors: %v", err)
	}
	sub, ok := v.(codegraph.Subgraph)
	if !ok || len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n1" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphTraceDataFlow(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{dataFlowTrace: codegraph.DataFlowTrace{
			SymbolID: "cls", FieldName: "total",
			Steps: []codegraph.DataFlowStep{{NodeID: "m1", Kind: "write"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphTraceDataFlow(context.Background(), nil, codeGraphTraceDataFlowInput{SymbolID: "cls", FieldName: "total"})
	if err != nil {
		t.Fatalf("toolCodeGraphTraceDataFlow: %v", err)
	}
	trace, ok := v.(codegraph.DataFlowTrace)
	if !ok || len(trace.Steps) != 1 || trace.Steps[0].Kind != "write" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
