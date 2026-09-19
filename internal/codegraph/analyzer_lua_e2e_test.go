// internal/codegraph/analyzer_lua_e2e_test.go
package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything the module asks of an index, asked of a Lua one.
//
// The analyzer's own tests check what it produces. These check that what it
// produces is the shape the rest of CodeGraph reads: the flow walker, the
// blast radius, go-to-definition, find-references and the data-flow trace were
// all written against the Java analyzer's ids and labels, and a second
// analyzer that got that contract subtly wrong would pass every test in
// analyzer_lua_test.go and still leave half the panels empty.

func openLuaProject(t *testing.T, files map[string]string) (*Service, string) {
	t.Helper()
	root := writeLuaProject(t, files)

	service := newTestService(t, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(t.TempDir()))
	service.SetConfigDir(t.TempDir())
	t.Cleanup(func() { service.Close() })

	if err := service.OpenProjectDir(context.Background(), root); err != nil {
		t.Fatalf("OpenProjectDir: %v", err)
	}
	return service, root
}

// e2eFixture is a Kong plugin with one more hop than the analyzer tests use,
// so a flow has something to walk: handler → limiter → store.
var e2eFixture = map[string]string{
	"kong/plugins/quota/handler.lua": `
local limiter = require "kong.plugins.quota.limiter"

local Quota = { PRIORITY = 800, VERSION = "2.0.0" }

--- Refuses the request once the caller is over quota.
function Quota:access(conf)
  self.seen = (self.seen or 0) + 1
  if limiter.over(conf.consumer, conf.limit) then
    return kong.response.exit(429)
  end
end

return Quota
`,
	"kong/plugins/quota/limiter.lua": `
local store = require "kong.plugins.quota.store"

local Limiter = {}

--- Reports whether a consumer has used up its quota.
function Limiter.over(consumer, limit)
  return store.count(consumer) >= limit
end

return Limiter
`,
	"kong/plugins/quota/store.lua": `
local redis = require "resty.redis"

local Store = {}

function Store.count(consumer)
  local red = redis:new()
  return red:get(consumer) or 0
end

return Store
`,
}

func TestLuaFlowWalksTheWholeChain(t *testing.T) {
	service, _ := openLuaProject(t, e2eFixture)
	ctx := context.Background()

	endpoints, err := service.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	var access Endpoint
	for _, endpoint := range endpoints {
		if endpoint.HTTPMethod == "ACCESS" {
			access = endpoint
		}
	}
	if access.ID == "" {
		t.Fatalf("no ACCESS endpoint among %v", endpoints)
	}

	flow, err := service.GetApiFlow(ctx, access.ID)
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	var labels []string
	for _, step := range flow.Steps {
		labels = append(labels, step.Label)
	}
	joined := strings.Join(labels, " → ")
	for _, want := range []string{
		"kong.plugins.quota.handler.access",
		"kong.plugins.quota.limiter.over",
		"kong.plugins.quota.store.count",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("flow %q is missing %s", joined, want)
		}
	}
	// The doc comment travels with the step, which is what the flow shows
	// under each name.
	for _, step := range flow.Steps {
		if step.Label == "kong.plugins.quota.limiter.over" && step.Summary == "" {
			t.Error("expected the limiter's doc comment on its flow step")
		}
	}
}

func TestLuaBlastRadiusFollowsRequires(t *testing.T) {
	service, _ := openLuaProject(t, e2eFixture)

	affected, err := service.GetBlastRadius(context.Background(), "kong/plugins/quota/store.lua")
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	joined := strings.Join(affected, ",")
	for _, want := range []string{"kong/plugins/quota/limiter.lua", "kong/plugins/quota/handler.lua"} {
		if !strings.Contains(joined, want) {
			t.Errorf("blast radius %v should reach %s", affected, want)
		}
	}
}

// Go-to-definition works off labels and exact source lines, which is where a
// second analyzer's id scheme meets the code that reads it.
func TestLuaGoToDefinitionFromACallSite(t *testing.T) {
	service, root := openLuaProject(t, e2eFixture)

	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("kong/plugins/quota/limiter.lua")))
	if err != nil {
		t.Fatal(err)
	}
	line := 0
	for i, text := range strings.Split(string(source), "\n") {
		if strings.Contains(text, "store.count(") {
			line = i + 1
		}
	}
	if line == 0 {
		t.Fatal("could not find the call in the fixture")
	}

	definitions, err := service.FindDefinitions(context.Background(), "kong/plugins/quota/limiter.lua", line, "count")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(definitions) == 0 {
		t.Fatal("expected the call site to resolve to a definition")
	}
	if definitions[0].Label != "kong.plugins.quota.store.count" {
		t.Errorf("definition = %q, want the store's count", definitions[0].Label)
	}
	if definitions[0].FilePath != "kong/plugins/quota/store.lua" {
		t.Errorf("definition file = %q", definitions[0].FilePath)
	}
}

func TestLuaFindReferencesFindsCallersAndRequirers(t *testing.T) {
	service, root := openLuaProject(t, e2eFixture)

	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("kong/plugins/quota/store.lua")))
	if err != nil {
		t.Fatal(err)
	}
	line := 0
	for i, text := range strings.Split(string(source), "\n") {
		if strings.Contains(text, "function Store.count") {
			line = i + 1
		}
	}

	references, err := service.FindReferences(context.Background(), "kong/plugins/quota/store.lua", line, "count")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	found := false
	for _, ref := range references {
		if ref.Label == "kong.plugins.quota.limiter.over" && ref.Kind == "call" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the limiter among the references, got %+v", references)
	}
}

func TestLuaDataFlowFollowsAFieldOnTheModuleTable(t *testing.T) {
	service, _ := openLuaProject(t, e2eFixture)
	ctx := context.Background()

	matches, err := service.SearchSymbol(ctx, "kong.plugins.quota.handler")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	moduleID := ""
	for _, match := range matches {
		if match.Kind == "class" && match.Label == "kong.plugins.quota.handler" {
			moduleID = match.NodeID
		}
	}
	if moduleID == "" {
		t.Fatalf("no module node among %v", matches)
	}

	fields, err := service.ListFields(ctx, moduleID)
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	var seen *FieldRef
	for i := range fields {
		if fields[i].FieldName == "seen" {
			seen = &fields[i]
		}
	}
	if seen == nil {
		t.Fatalf("expected `seen` among the handler's fields, got %+v", fields)
	}

	trace, err := service.TraceDataFlow(ctx, seen.ClassID, "seen")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	kinds := map[string]bool{}
	for _, step := range trace.Steps {
		kinds[step.Kind] = true
		if step.Kind != "call" && step.Code == "" {
			t.Errorf("a trace step should carry its source line: %+v", step)
		}
	}
	if !kinds["read"] || !kinds["write"] {
		t.Errorf("expected both a read and a write of `seen`, got %+v", trace.Steps)
	}
}

func TestLuaNeighboursAreReachableFromAModule(t *testing.T) {
	service, _ := openLuaProject(t, e2eFixture)
	ctx := context.Background()

	matches, err := service.SearchSymbol(ctx, "quota.handler")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected the handler in the index")
	}

	subgraph, err := service.GetNeighbors(ctx, matches[0].NodeID, 2, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(subgraph.Nodes) < 2 {
		t.Errorf("expected the handler's neighbourhood, got %d nodes", len(subgraph.Nodes))
	}
}

// A code snippet is read back out of the file by the node's own line range, so
// an off-by-one in the parser's positions shows up here and nowhere else.
func TestLuaSnippetMatchesTheFunctionItNames(t *testing.T) {
	service, _ := openLuaProject(t, e2eFixture)
	ctx := context.Background()

	matches, err := service.SearchSymbol(ctx, "over")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	var target SymbolMatch
	for _, match := range matches {
		if match.Label == "kong.plugins.quota.limiter.over" {
			target = match
		}
	}
	if target.NodeID == "" {
		t.Fatalf("no node for the limiter's over among %v", matches)
	}

	snippet, err := service.GetCodeSnippet(ctx, target.FilePath, target.Line, 0)
	if err != nil {
		t.Fatalf("GetCodeSnippet: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(snippet.Source), "function Limiter.over(") {
		t.Errorf("snippet starts with %q, want the function it names", strings.SplitN(snippet.Source, "\n", 2)[0])
	}
	if !strings.HasSuffix(strings.TrimSpace(snippet.Source), "end") {
		t.Errorf("snippet should end at the function's end, got %q", snippet.Source)
	}
	if snippet.Language != "lua" {
		t.Errorf("snippet language = %q, want lua", snippet.Language)
	}
}
