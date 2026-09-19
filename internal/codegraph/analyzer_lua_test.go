// internal/codegraph/analyzer_lua_test.go
package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLuaProject lays a fixture tree out on disk and returns its root. The
// analyzer reads files, so the tests give it files — a fake filesystem would
// only be testing the fake.
func writeLuaProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func analyzeLua(t *testing.T, files map[string]string) AnalysisResult {
	t.Helper()
	result, err := NewLuaAnalyzer().Analyze(context.Background(), writeLuaProject(t, files))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	// Every result the analyzer hands back crosses the same boundary the Java
	// one does, and the same check guards it.
	if err := result.Validate(); err != nil {
		t.Fatalf("the analyzer produced an inconsistent index: %v", err)
	}
	return result
}

func nodeByLabel(t *testing.T, result AnalysisResult, label string) Node {
	t.Helper()
	for _, node := range result.Nodes {
		if node.Label == label {
			return node
		}
	}
	var labels []string
	for _, node := range result.Nodes {
		labels = append(labels, node.Label)
	}
	t.Fatalf("no node labelled %q; have %v", label, labels)
	return Node{}
}

func hasEdge(result AnalysisResult, fromLabel, toLabel, kind string) bool {
	byID := map[string]string{}
	for _, node := range result.Nodes {
		byID[node.ID] = node.Label
	}
	for _, edge := range result.Edges {
		if byID[edge.From] == fromLabel && byID[edge.To] == toLabel && edge.Kind == kind {
			return true
		}
	}
	return false
}

// kongPluginFixture is the shape the module was built for: a plugin with a
// handler, a policy module that talks to Redis, an Admin API and a schema.
var kongPluginFixture = map[string]string{
	"kong/plugins/rate-limit/handler.lua": `
--- Rate limits requests per consumer.
-- Backed by whichever policy the config names.
local policies = require "kong.plugins.rate-limit.policies"

local RateLimit = {
  PRIORITY = 900,
  VERSION = "1.0.0",
}

--- Counts the request and refuses it past the limit.
function RateLimit:access(conf)
  self.counted = (self.counted or 0) + 1
  local hits = policies.incr(conf.key, conf.window)
  if hits > conf.limit then
    return kong.response.exit(429, { message = "too many requests" })
  end
end

function RateLimit:log(conf)
  policies.flush()
  self:report()
end

function RateLimit:report()
  kong.log.info("counted ", self.counted)
end

return RateLimit
`,
	"kong/plugins/rate-limit/policies/init.lua": `
local redis = require "resty.redis"

local Policies = {}

--- Increments the window counter and returns the new value.
function Policies.incr(key, window)
  local red = redis:new()
  red:connect("127.0.0.1", 6379)
  return red:incr(key)
end

function Policies.flush()
  return true
end

return Policies
`,
	"kong/plugins/rate-limit/api.lua": `
return {
  ["/rate-limit/status"] = {
    GET = function(self)
      return { status = "ok" }
    end,
  },
}
`,
	"kong/plugins/rate-limit/schema.lua": `
return {
  name = "rate-limit",
  fields = { { config = { type = "record" } } },
}
`,
}

func TestLuaAnalyzerReadsAKongPlugin(t *testing.T) {
	result := analyzeLua(t, kongPluginFixture)

	handler := nodeByLabel(t, result, "kong.plugins.rate-limit.handler")
	if handler.Kind != "class" {
		t.Errorf("handler module kind = %q, want class", handler.Kind)
	}
	if handler.Layer != "controller" {
		t.Errorf("handler layer = %q, want controller", handler.Layer)
	}
	if !strings.HasPrefix(handler.Summary, "Rate limits requests per consumer.") {
		t.Errorf("handler summary = %q, want the file's header comment", handler.Summary)
	}
	if handler.FilePath != "kong/plugins/rate-limit/handler.lua" {
		t.Errorf("handler file = %q", handler.FilePath)
	}

	// A method declared with a colon is one function, whose first parameter is
	// the self the source never writes.
	access := nodeByLabel(t, result, "kong.plugins.rate-limit.handler.access")
	if access.Kind != "method" {
		t.Errorf("access kind = %q, want method", access.Kind)
	}
	if len(access.Params) != 2 || access.Params[0].Name != "self" || access.Params[1].Name != "conf" {
		t.Errorf("access params = %v, want self and conf", access.Params)
	}
	if access.Summary != "Counts the request and refuses it past the limit." {
		t.Errorf("access summary = %q", access.Summary)
	}
	if access.Layer != "controller" {
		t.Errorf("access layer = %q, want its module's", access.Layer)
	}

	// A module that requires resty.redis is where this project's data is.
	policies := nodeByLabel(t, result, "kong.plugins.rate-limit.policies")
	if policies.Layer != "repository" {
		t.Errorf("policies layer = %q, want repository", policies.Layer)
	}
}

func TestLuaAnalyzerResolvesCallsAcrossModules(t *testing.T) {
	result := analyzeLua(t, kongPluginFixture)

	if !hasEdge(result, "kong.plugins.rate-limit.handler.access", "kong.plugins.rate-limit.policies.incr", "call") {
		t.Error("expected access to call the policy module's incr")
	}
	if !hasEdge(result, "kong.plugins.rate-limit.handler.log", "kong.plugins.rate-limit.policies.flush", "call") {
		t.Error("expected log to call the policy module's flush")
	}
	// self:report() inside the same table, through the implicit self.
	if !hasEdge(result, "kong.plugins.rate-limit.handler.log", "kong.plugins.rate-limit.handler.report", "call") {
		t.Error("expected self:report() to resolve within the module")
	}
	// require is a dependency between modules, which is what a "di" edge is.
	if !hasEdge(result, "kong.plugins.rate-limit.handler", "kong.plugins.rate-limit.policies", "di") {
		t.Error("expected the require to be recorded as a module dependency")
	}

	var depended bool
	for _, dep := range result.FileDependencies {
		if dep.FilePath == "kong/plugins/rate-limit/handler.lua" &&
			dep.DependsOnFilePath == "kong/plugins/rate-limit/policies/init.lua" {
			depended = true
		}
	}
	if !depended {
		t.Errorf("expected a file dependency through the require; got %v", result.FileDependencies)
	}
}

// The reason a call could not be followed is the useful half of recording it.
func TestLuaAnalyzerNamesTheWallItHit(t *testing.T) {
	result := analyzeLua(t, kongPluginFixture)

	byID := map[string]Node{}
	for _, node := range result.Nodes {
		byID[node.ID] = node
	}
	reasons := map[string]string{}
	for _, site := range result.CallSites {
		if site.Unresolved == "" {
			continue
		}
		reasons[byID[site.From].Label+"@"+site.Unresolved] = site.Unresolved
	}

	// kong.* is a global of the runtime, not a gap in the analysis.
	if reasons["kong.plugins.rate-limit.handler.access@library"] == "" {
		t.Errorf("expected kong.response.exit to read as library; reasons: %v", reasons)
	}
	// A redis client is a required module with no file in this project, and
	// everything taken off it stays outside the project too.
	if reasons["kong.plugins.rate-limit.policies.incr@library"] == "" {
		t.Errorf("expected the redis client's calls to read as library; reasons: %v", reasons)
	}
}

func TestLuaAnalyzerRecordsFieldsOnTheModuleTable(t *testing.T) {
	result := analyzeLua(t, kongPluginFixture)

	handler := nodeByLabel(t, result, "kong.plugins.rate-limit.handler")
	access := nodeByLabel(t, result, "kong.plugins.rate-limit.handler.access")

	var reads, writes int
	for _, fa := range result.FieldAccesses {
		if fa.ClassID != handler.ID || fa.FieldName != "counted" || fa.NodeID != access.ID {
			continue
		}
		switch fa.Kind {
		case "read":
			reads++
		case "write":
			writes++
		}
	}
	if writes != 1 || reads != 1 {
		t.Errorf("counted: %d reads and %d writes, want 1 of each; accesses: %v", reads, writes, result.FieldAccesses)
	}

	// A declared function is not a field, however it is reached.
	for _, fa := range result.FieldAccesses {
		if fa.FieldName == "report" || fa.FieldName == "access" {
			t.Errorf("a declared function was recorded as a field: %v", fa)
		}
	}
}

func TestLuaAnalyzerFindsKongEntryPoints(t *testing.T) {
	result := analyzeLua(t, kongPluginFixture)

	found := map[string]string{}
	byID := map[string]Node{}
	for _, node := range result.Nodes {
		byID[node.ID] = node
	}
	for _, endpoint := range result.Endpoints {
		found[endpoint.HTTPMethod+" "+endpoint.Path] = byID[endpoint.EntryNode].Label
	}

	// A plugin phase is an entry point with no route of its own: the phase is
	// the method and the plugin is what it belongs to.
	if got := found["ACCESS rate-limit"]; got != "kong.plugins.rate-limit.handler.access" {
		t.Errorf("ACCESS entry node = %q; endpoints: %v", got, found)
	}
	if got := found["LOG rate-limit"]; got != "kong.plugins.rate-limit.handler.log" {
		t.Errorf("LOG entry node = %q; endpoints: %v", got, found)
	}
	// A function that merely exists on the table is not a phase.
	if _, reported := found["REPORT rate-limit"]; reported {
		t.Error("report is not a Kong phase and must not be an endpoint")
	}
	// The Admin API is a real route.
	if got := found["GET /rate-limit/status"]; got == "" {
		t.Errorf("expected the Admin API route; endpoints: %v", found)
	}
	// schema.lua declares no entry point and must not invent one.
	for _, endpoint := range result.Endpoints {
		if strings.Contains(byID[endpoint.EntryNode].FilePath, "schema.lua") {
			t.Errorf("schema.lua should declare no endpoint, got %+v", endpoint)
		}
	}
}

// Every way a module puts a function on its table has to resolve, because a
// project mixes them and a caller cannot tell which was used.
func TestLuaAnalyzerReadsEveryDeclarationStyle(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"colon.lua":  "local M = {}\nfunction M:one() end\nreturn M\n",
		"dotted.lua": "local M = {}\nfunction M.two() end\nreturn M\n",
		"assign.lua": "local M = {}\nM.three = function() end\nreturn M\n",
		"literal.lua": `local M = {
  four = function() end,
}
return M
`,
		"inline.lua": "return {\n  five = function() end,\n}\n",
		"global.lua": "function six() end\n",
		"caller.lua": `
local colon = require "colon"
local dotted = require "dotted"
local assign = require "assign"
local literal = require "literal"
local inline = require "inline"

local function callThemAll()
  colon:one()
  dotted.two()
  assign.three()
  literal.four()
  inline.five()
end

return { callThemAll = callThemAll }
`,
	})

	for _, target := range []string{"colon.one", "dotted.two", "assign.three", "literal.four", "inline.five"} {
		if !hasEdge(result, "caller.callThemAll", target, "call") {
			t.Errorf("expected caller.callThemAll to reach %s", target)
		}
	}
	// A global declaration is still a node, and still exported.
	nodeByLabel(t, result, "global.six")
}

func TestLuaAnalyzerFollowsAConstructorButNotAnyCall(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"session.lua": `
local Session = {}

function Session.new(id)
  return setmetatable({ id = id }, { __index = Session })
end

function Session.close(self)
  return true
end

function Session.rows(self)
  return {}
end

return Session
`,
		"user.lua": `
local Session = require "session"

local function work()
  local s = Session.new(1)
  s.close()
  local rows = s.rows()
  rows.close()
end

return { work = work }
`,
	})

	// `Session.new(...)` hands back a Session, so the call on it resolves.
	if !hasEdge(result, "user.work", "session.close", "call") {
		t.Error("expected a call on a constructed instance to resolve")
	}
	// `s.rows()` hands back data, not a Session. Following it would resolve
	// rows.close() to Session.close as well — a second call site pointing at a
	// function that never runs, which is the fabrication this guards.
	if got := countCallSites(result, "user.work", "session.close"); got != 1 {
		t.Errorf("call sites reaching session.close = %d, want exactly the one written", got)
	}
}

// countCallSites counts the individual call expressions resolved from one node
// to another — unlike edges, which are deduped into one per relationship.
func countCallSites(result AnalysisResult, fromLabel, toLabel string) int {
	byID := map[string]string{}
	for _, node := range result.Nodes {
		byID[node.ID] = node.Label
	}
	count := 0
	for _, site := range result.CallSites {
		if byID[site.From] == fromLabel && site.To != "" && byID[site.To] == toLabel {
			count++
		}
	}
	return count
}

func TestLuaAnalyzerLayersWhatAControllerReachesFor(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"kong/plugins/audit/handler.lua": `
local emitter = require "kong.plugins.audit.emitter"

local Audit = { PRIORITY = 10, VERSION = "1.0" }

function Audit:log(conf)
  emitter.send(conf)
end

return Audit
`,
		"kong/plugins/audit/emitter.lua": "local M = {}\nfunction M.send(conf) end\nreturn M\n",
	})

	emitter := nodeByLabel(t, result, "kong.plugins.audit.emitter")
	if emitter.Layer != "service" {
		t.Errorf("emitter layer = %q, want service — it is what the controller reaches for", emitter.Layer)
	}
}

func TestLuaAnalyzerFindsLapisRoutes(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"app.lua": `
local lapis = require "lapis"
local app = lapis.Application()

app:get("/health", function(self)
  return { status = "ok" }
end)

app:post("/orders", function(self)
  return { created = true }
end)

return app
`,
	})

	found := map[string]bool{}
	for _, endpoint := range result.Endpoints {
		found[endpoint.HTTPMethod+" "+endpoint.Path] = true
	}
	if !found["GET /health"] || !found["POST /orders"] {
		t.Errorf("expected both Lapis routes, got %v", found)
	}
	if nodeByLabel(t, result, "app").Layer != "controller" {
		t.Error("a file declaring routes is a controller")
	}
}

// A require path written the way it is deployed still has to find the file
// sitting at the repository's own root.
func TestLuaAnalyzerResolvesADeployedRequirePathToALocalFile(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"handler.lua": `
local util = require "kong.plugins.mine.util"
local M = { PRIORITY = 1, VERSION = "1.0" }
function M:access(conf)
  util.clean(conf)
end
return M
`,
		"util.lua": "local M = {}\nfunction M.clean(conf) end\nreturn M\n",
	})

	if !hasEdge(result, "handler.access", "util.clean", "call") {
		t.Error("expected the deployed require path to resolve to the file at the root")
	}
}

func TestLuaAnalyzerKeepsABrokenFilesOtherFunctions(t *testing.T) {
	result := analyzeLua(t, map[string]string{
		"broken.lua": `
local M = {}

function M.before()
  return 1
end

local = = =

function M.after()
  return 2
end

return M
`,
	})

	nodeByLabel(t, result, "broken.before")
	nodeByLabel(t, result, "broken.after")
}

func TestLuaRequirePathConventions(t *testing.T) {
	for _, tc := range []struct{ rel, want string }{
		{"handler.lua", "handler"},
		{"kong/plugins/x/handler.lua", "kong.plugins.x.handler"},
		{"kong/plugins/x/policies/init.lua", "kong.plugins.x.policies"},
		{"lua/mine/config.lua", "mine.config"},
		{"src/mine/config.lua", "mine.config"},
	} {
		if got := luaRequirePath(tc.rel); got != tc.want {
			t.Errorf("luaRequirePath(%q) = %q, want %q", tc.rel, got, tc.want)
		}
	}
}

func TestLuaAnalyzerIsDeterministic(t *testing.T) {
	root := writeLuaProject(t, kongPluginFixture)
	first, err := NewLuaAnalyzer().Analyze(context.Background(), root)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	second, err := NewLuaAnalyzer().Analyze(context.Background(), root)
	if err != nil {
		t.Fatalf("analyze again: %v", err)
	}

	if len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("node counts differ: %d then %d", len(first.Nodes), len(second.Nodes))
	}
	for i := range first.Nodes {
		if first.Nodes[i].ID != second.Nodes[i].ID {
			t.Fatalf("node %d differs between runs: %q then %q", i, first.Nodes[i].ID, second.Nodes[i].ID)
		}
	}
	for i := range first.Endpoints {
		if first.Endpoints[i].ID != second.Endpoints[i].ID {
			t.Fatalf("endpoint %d differs between runs: %q then %q", i, first.Endpoints[i].ID, second.Endpoints[i].ID)
		}
	}
}

func TestLuaAnalyzerOnAFolderWithNoLua(t *testing.T) {
	result, err := NewLuaAnalyzer().Analyze(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("an empty folder is not an error: %v", err)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("expected no nodes, got %d", len(result.Nodes))
	}
}
