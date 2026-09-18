// internal/mcp/tools_codegraph_test.go
package mcp

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/capabilities"
	"devkit/internal/codegraph"
)

type fakeCodeGraphAPI struct {
	openProjectErr error
	detection      codegraph.DetectedProject
	endpoints      []codegraph.Endpoint
	flow           codegraph.ApiFlow
	matches        []codegraph.SymbolMatch
	files          []codegraph.FileEntry
	fileContent    string
	snippet        codegraph.CodeSnippet
	blastRadius    []string
	indexVersion   int
	subgraph       codegraph.Subgraph
	dataFlowTrace  codegraph.DataFlowTrace
	fieldRefs      []codegraph.FieldRef
	comments       []codegraph.PlanComment
	plan           codegraph.Plan
	planProgress   codegraph.PlanProgress
	err            error
}

func (f fakeCodeGraphAPI) OpenProjectDir(ctx context.Context, projectPath string) error {
	return f.openProjectErr
}
func (f fakeCodeGraphAPI) ActiveDetection() codegraph.DetectedProject {
	return f.detection
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
func (f fakeCodeGraphAPI) ReplyPlanNode(ctx context.Context, planNodeID, body, sessionID string) error {
	return f.err
}
func (f fakeCodeGraphAPI) ListPlanNodeComments(ctx context.Context, planNodeID string) ([]codegraph.PlanComment, error) {
	return f.comments, f.err
}
func (f fakeCodeGraphAPI) ListFields(ctx context.Context, nodeID string) ([]codegraph.FieldRef, error) {
	return f.fieldRefs, f.err
}
func (f fakeCodeGraphAPI) ProposeFlow(ctx context.Context, plan codegraph.Plan) (codegraph.PlanProgress, error) {
	return f.planProgress, f.err
}
func (f fakeCodeGraphAPI) GetPlannedFlow(ctx context.Context) (codegraph.Plan, error) {
	return f.plan, f.err
}
func (f fakeCodeGraphAPI) GetPlanProgress(ctx context.Context) (codegraph.PlanProgress, error) {
	return f.planProgress, f.err
}
func (f fakeCodeGraphAPI) RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error {
	return f.err
}

func TestToolCodeGraphOpenProject(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{detection: codegraph.DetectedProject{Language: "java", Label: "Maven", Modules: []string{"."}}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphOpenProject(context.Background(), nil, codeGraphOpenProjectInput{Path: "/abs/project"})
	if err != nil {
		t.Fatalf("toolCodeGraphOpenProject: %v", err)
	}
	detected, ok := unwrap(v).(codegraph.DetectedProject)
	if !ok || detected.Language != "java" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

func TestToolCodeGraphOpenProjectRequiresAbsolutePath(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	if _, _, err := s.toolCodeGraphOpenProject(context.Background(), nil, codeGraphOpenProjectInput{Path: "relative/dir"}); err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestToolCodeGraphOpenProjectRequiresPath(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	if _, _, err := s.toolCodeGraphOpenProject(context.Background(), nil, codeGraphOpenProjectInput{}); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestToolCodeGraphOpenProjectPropagatesError(t *testing.T) {
	wantErr := codegraph.ErrUnsupportedProject
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{openProjectErr: wantErr},
	}
	s := NewServer(deps)
	_, _, err := s.toolCodeGraphOpenProject(context.Background(), nil, codeGraphOpenProjectInput{Path: "/abs/project"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
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
	endpoints, ok := unwrap(v).([]codegraph.Endpoint)
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
	content, ok := unwrap(v).(string)
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
	radius, ok := unwrap(v).([]string)
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
	version, ok := unwrap(v).(int)
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
	sub, ok := unwrap(v).(codegraph.Subgraph)
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
	trace, ok := unwrap(v).(codegraph.DataFlowTrace)
	if !ok || len(trace.Steps) != 1 || trace.Steps[0].Kind != "write" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

type gatewayCtxKey struct{}

// stampingCaller hands its handler a context the tool's own ctx does not
// carry — the way the real gateway attaches the caller's identity and the
// call's deadline.
type stampingCaller struct{}

func (stampingCaller) Call(ctx context.Context, _, _, _ string, _ map[string]any, handler capabilities.Handler) (any, error) {
	return handler(context.WithValue(ctx, gatewayCtxKey{}, "gateway"))
}

type ctxRecordingCodeGraph struct {
	fakeCodeGraphAPI
	seen *any
}

func (f ctxRecordingCodeGraph) ListEndpoints(ctx context.Context) ([]codegraph.Endpoint, error) {
	*f.seen = ctx.Value(gatewayCtxKey{})
	return nil, nil
}

// The handler must run on the context the gateway gives it, not the one the
// tool was called with — otherwise the gateway's deadline and identity never
// reach the service.
func TestCodeGraphToolsRunOnTheGatewayContext(t *testing.T) {
	var seen any
	s := NewServer(Deps{Caller: stampingCaller{}, CodeGraph: ctxRecordingCodeGraph{seen: &seen}})
	if _, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if seen != "gateway" {
		t.Fatalf("handler ran on the tool's own ctx, not the gateway's (saw %v)", seen)
	}
}
