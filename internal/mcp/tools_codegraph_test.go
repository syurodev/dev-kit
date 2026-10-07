// internal/mcp/tools_codegraph_test.go
package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/capabilities"
	"devkit/internal/codegraph"
)

type fakeCodeGraphAPI struct {
	openProjectErr error
	detection      codegraph.DetectedProject
	endpoints      []codegraph.EntryPoint
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
func (f fakeCodeGraphAPI) ListEntryPointsPage(ctx context.Context, q codegraph.EntryPointQuery) (codegraph.EntryPointPage, error) {
	if f.err != nil {
		return codegraph.EntryPointPage{}, f.err
	}
	return codegraph.PageEntryPoints(f.endpoints, q)
}
func (f fakeCodeGraphAPI) GetApiFlow(ctx context.Context, id string) (codegraph.ApiFlow, error) {
	return f.flow, f.err
}
func (f fakeCodeGraphAPI) SearchSymbolsPage(ctx context.Context, q codegraph.SymbolQuery) (codegraph.SymbolPage, error) {
	return codegraph.SymbolPage{Result: f.matches, Total: len(f.matches)}, f.err
}
func (f fakeCodeGraphAPI) FindConfigUsage(ctx context.Context, q codegraph.ConfigUsageQuery) (codegraph.ConfigUsageReport, error) {
	return codegraph.ConfigUsageReport{Query: q.Key, Canonical: codegraph.CanonicalConfigKey(q.Key)}, f.err
}
func (f fakeCodeGraphAPI) FindTableUsage(ctx context.Context, q codegraph.TableUsageQuery) (codegraph.TableUsageReport, error) {
	return codegraph.TableUsageReport{Query: q.Table, Canonical: codegraph.CanonicalConfigKey(q.Table)}, f.err
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
		CodeGraph: fakeCodeGraphAPI{endpoints: []codegraph.EntryPoint{{ID: "ep1", Target: "/orders"}}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{})
	if err != nil {
		t.Fatalf("toolCodeGraphListEndpoints: %v", err)
	}
	page, ok := unwrap(v).(codegraph.EntryPointPage)
	if !ok || len(page.Result) != 1 || page.Result[0].Target != "/orders" || page.Total != 1 {
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
	_, v, err := s.toolCodeGraphWaitForIndexReady(context.Background(), nil, codeGraphServiceInput{})
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

func (f ctxRecordingCodeGraph) ListEntryPointsPage(ctx context.Context, _ codegraph.EntryPointQuery) (codegraph.EntryPointPage, error) {
	*f.seen = ctx.Value(gatewayCtxKey{})
	return codegraph.EntryPointPage{}, nil
}

// The handler must run on the context the gateway gives it, not the one the
// tool was called with — otherwise the gateway's deadline and identity never
// reach the service.
func TestCodeGraphToolsRunOnTheGatewayContext(t *testing.T) {
	var seen any
	s := NewServer(Deps{Caller: stampingCaller{}, CodeGraph: ctxRecordingCodeGraph{seen: &seen}})
	if _, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{}); err != nil {
		t.Fatal(err)
	}
	if seen != "gateway" {
		t.Fatalf("handler ran on the tool's own ctx, not the gateway's (saw %v)", seen)
	}
}

type fakeCodeGraphServices struct {
	byPath    map[string]CodeGraphAPI
	focused   []string
	snapshots map[string]codegraph.ServiceSnapshot
}

func (f *fakeCodeGraphServices) Service(_ context.Context, path string) (CodeGraphAPI, error) {
	svc, ok := f.byPath[path]
	if !ok {
		return nil, errors.New("no such service")
	}
	return svc, nil
}

func (f *fakeCodeGraphServices) Snapshot(_ context.Context, path string) (codegraph.ServiceSnapshot, error) {
	if snap, ok := f.snapshots[path]; ok {
		return snap, nil
	}
	return codegraph.ServiceSnapshot{Path: path, State: codegraph.SnapshotNotIndexed}, nil
}

func (f *fakeCodeGraphServices) Focus(_ context.Context, path string) (CodeGraphAPI, error) {
	f.focused = append(f.focused, path)
	return fakeCodeGraphAPI{detection: codegraph.DetectedProject{Label: "focused"}}, nil
}

// servicePath reads a workspace service; empty reads the focused project; a
// relative path, or one with no Registry wired, is refused.
func TestToolCodeGraphServicePath(t *testing.T) {
	services := &fakeCodeGraphServices{byPath: map[string]CodeGraphAPI{
		"/ws/billing": fakeCodeGraphAPI{endpoints: []codegraph.EntryPoint{{ID: "b", Target: "/refunds"}}},
	}}
	s := NewServer(Deps{
		Caller:            &fakeCaller{},
		CodeGraph:         fakeCodeGraphAPI{endpoints: []codegraph.EntryPoint{{ID: "o", Target: "/orders"}}},
		CodeGraphServices: services,
	})
	path := func(in codeGraphListEndpointsInput) string {
		t.Helper()
		_, v, err := s.toolCodeGraphListEndpoints(context.Background(), nil, in)
		if err != nil {
			t.Fatal(err)
		}
		return unwrap(v).(codegraph.EntryPointPage).Result[0].Target
	}
	if got := path(codeGraphListEndpointsInput{}); got != "/orders" {
		t.Fatalf("focused = %q", got)
	}
	if got := path(codeGraphListEndpointsInput{ServicePath: "/ws/billing"}); got != "/refunds" {
		t.Fatalf("named = %q", got)
	}
	if _, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{ServicePath: "billing"}); err == nil {
		t.Fatal("a relative servicePath must be refused")
	}

	if _, _, err := s.toolCodeGraphOpenProject(context.Background(), nil, codeGraphOpenProjectInput{Path: "/ws/billing"}); err != nil {
		t.Fatal(err)
	}
	if len(services.focused) != 1 || services.focused[0] != "/ws/billing" {
		t.Fatalf("openProject must focus through the Registry, got %v", services.focused)
	}

	bare := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	if _, _, err := bare.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{ServicePath: "/ws/billing"}); err == nil {
		t.Fatal("with no Registry wired a servicePath must be refused")
	}
}

// waitingCodeGraph is "not ready" until the index finishes, the way the real
// service answers while it is still building.
type waitingCodeGraph struct {
	fakeCodeGraphAPI
	notReady int   // queries that answer ErrIndexNotReady before one succeeds
	waitErr  error // what WaitForIndexReady returns
	queries  int
	waits    int
}

func (f *waitingCodeGraph) ListEntryPointsPage(ctx context.Context, q codegraph.EntryPointQuery) (codegraph.EntryPointPage, error) {
	f.queries++
	if f.queries <= f.notReady {
		return codegraph.EntryPointPage{}, codegraph.ErrIndexNotReady
	}
	return codegraph.PageEntryPoints(f.endpoints, q)
}

func (f *waitingCodeGraph) WaitForIndexReady(ctx context.Context) (int, error) {
	f.waits++
	return 1, f.waitErr
}

// A query that lands while the index is building waits for it and answers,
// instead of making the agent call waitForIndexReady by hand.
func TestCodeGraphQueryWaitsForTheIndex(t *testing.T) {
	cg := &waitingCodeGraph{notReady: 1, fakeCodeGraphAPI: fakeCodeGraphAPI{endpoints: []codegraph.EntryPoint{{ID: "ep1", Target: "/orders"}}}}
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: cg})
	_, v, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if page := unwrap(v).(codegraph.EntryPointPage); len(page.Result) != 1 {
		t.Fatalf("page = %+v", page)
	}
	if cg.waits != 1 || cg.queries != 2 {
		t.Fatalf("waits = %d, queries = %d, want 1 and 2", cg.waits, cg.queries)
	}
}

// A ready index is not waited for.
func TestCodeGraphQueryDoesNotWaitWhenReady(t *testing.T) {
	cg := &waitingCodeGraph{}
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: cg})
	if _, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{}); err != nil {
		t.Fatal(err)
	}
	if cg.waits != 0 {
		t.Fatalf("waited %d times for a ready index", cg.waits)
	}
}

// A slow index and a failed one both stay errors, and say different things.
func TestCodeGraphQueryExplainsWhyItGaveUp(t *testing.T) {
	slow := &waitingCodeGraph{notReady: 99, waitErr: context.DeadlineExceeded}
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: slow})
	_, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{})
	if !errors.Is(err, codegraph.ErrIndexNotReady) || !strings.Contains(err.Error(), "codegraph.waitForIndexReady") {
		t.Fatalf("slow index: err = %v", err)
	}

	failed := &waitingCodeGraph{notReady: 99, waitErr: fmt.Errorf("%w: analyzer exited 1", codegraph.ErrIndexNotReady)}
	s = NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: failed})
	_, _, err = s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{})
	if !errors.Is(err, codegraph.ErrIndexNotReady) || !strings.Contains(err.Error(), "analyzer exited 1") || !strings.Contains(err.Error(), "codegraph.openProject") {
		t.Fatalf("failed index: err = %v", err)
	}
}

// Anything but "not ready" passes straight through, unwaited.
func TestCodeGraphQueryPassesOtherErrorsThrough(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{err: codegraph.ErrNoActiveProject}})
	if _, _, err := s.toolCodeGraphListEndpoints(context.Background(), nil, codeGraphListEndpointsInput{}); !errors.Is(err, codegraph.ErrNoActiveProject) {
		t.Fatalf("err = %v", err)
	}
}

func TestToolCodeGraphFindConfigUsage(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	_, v, err := s.toolCodeGraphFindConfigUsage(context.Background(), nil, codeGraphFindConfigUsageInput{Key: "FSAP_ACCESS_MODULE_BASED_ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := unwrap(v).(codegraph.ConfigUsageReport)
	if !ok || report.Canonical != "fsapaccessmodulebasedenabled" {
		t.Fatalf("unexpected result: %#v", v)
	}
}

// It waits for the index like every other graph read.
func TestFindConfigUsageWaitsForTheIndex(t *testing.T) {
	cg := &configWaiting{}
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: cg})
	if _, _, err := s.toolCodeGraphFindConfigUsage(context.Background(), nil, codeGraphFindConfigUsageInput{Key: "a.b"}); err != nil {
		t.Fatal(err)
	}
	if cg.waits != 1 || cg.queries != 2 {
		t.Fatalf("waits = %d, queries = %d, want 1 and 2", cg.waits, cg.queries)
	}
}

type configWaiting struct {
	fakeCodeGraphAPI
	queries, waits int
}

func (f *configWaiting) FindConfigUsage(ctx context.Context, q codegraph.ConfigUsageQuery) (codegraph.ConfigUsageReport, error) {
	f.queries++
	if f.queries == 1 {
		return codegraph.ConfigUsageReport{}, codegraph.ErrIndexNotReady
	}
	return codegraph.ConfigUsageReport{}, nil
}

func (f *configWaiting) WaitForIndexReady(ctx context.Context) (int, error) {
	f.waits++
	return 1, nil
}

func TestToolCodeGraphFindTableUsage(t *testing.T) {
	s := NewServer(Deps{Caller: &fakeCaller{}, CodeGraph: fakeCodeGraphAPI{}})
	_, v, err := s.toolCodeGraphFindTableUsage(context.Background(), nil, codeGraphFindTableUsageInput{Table: "organization_api_enablement"})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := unwrap(v).(codegraph.TableUsageReport)
	if !ok || report.Canonical != "organizationapienablement" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
