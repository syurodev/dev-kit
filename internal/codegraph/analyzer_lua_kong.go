// internal/codegraph/analyzer_lua_kong.go
package codegraph

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"devkit/internal/lua"
)

// Everything the Lua analyzer knows about what a Lua project is FOR lives
// here: where its entry points are, and what a file's job is.
//
// The Java analyzer reads Spring's annotations for both. Lua has no
// annotations and no framework everyone shares, so this reads the conventions
// of the one place Lua is written as a service: OpenResty, and Kong on top of
// it. A Kong plugin declares its entry points by naming functions after
// request phases; its Admin API declares them as a table keyed by route; Lapis
// declares them as calls on an app. All three are here, and a project that
// uses none of them simply has no endpoints — which is the honest answer, not
// a failure.

// kongPhases are the request-lifecycle functions a Kong plugin implements.
// Listed in the order Kong runs them, because that order is the only useful
// one to read a plugin's entry points in — alphabetical would put `access`
// after a filter that cannot run before it.
var kongPhases = []string{
	"init_worker", "configure", "certificate",
	"rewrite", "preread", "access",
	"ws_handshake", "ws_client_frame", "ws_upstream_frame", "ws_close",
	"header_filter", "body_filter", "response", "log",
}

// luaHTTPMethods are the keys a Kong Admin API route table uses for its
// handlers, and the names Lapis's routing methods carry.
var luaHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

// lapisRouteMethods are the calls a Lapis app declares a route with. `match`
// takes any HTTP method, which is what "ANY" means here — the same word the
// Java analyzer uses for a @RequestMapping that named none.
var lapisRouteMethods = map[string]string{
	"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH",
	"delete": "DELETE", "head": "HEAD", "options": "OPTIONS", "match": "ANY",
}

// luaDataAccessModules are the require paths that make a file a repository:
// the OpenResty database and cache clients, and Kong's own DAO. A file that
// pulls one of these in is where this project's data lives, which is what the
// layer colour is for.
var luaDataAccessModules = []string{
	"resty.redis", "resty.rediscluster", "resty.redis.connector",
	"resty.mysql", "resty.postgres", "pgmoon", "luasql",
	"resty.mongol", "resty.cassandra", "cassandra",
	"resty.lrucache", "resty.mlcache", "lapis.db", "kong.db",
}

// luaRepositoryNames and luaServiceNames are the fallback: a project that
// wraps its client in a module of its own still says what that module is, in
// the only place Lua has to say it — the file's name.
var (
	luaRepositoryNames = map[string]bool{
		"dao": true, "daos": true, "repository": true, "repositories": true,
		"store": true, "storage": true, "db": true, "database": true,
		"migrations": true, "cache": true,
	}
	luaServiceNames = map[string]bool{
		"service": true, "services": true, "usecase": true, "usecases": true,
		"domain": true, "logic": true, "core": true,
	}
)

// classifyLuaLayers decides every module's layer and collects the project's
// endpoints. One pass, because the two answers depend on each other: a module
// with endpoints is a controller, and what a controller requires is, in the
// absence of any other evidence, this project's service layer.
func classifyLuaLayers(modules []*luaModule, index *luaIndex, result *AnalysisResult) {
	entryIDs := map[string]bool{}
	for _, module := range modules {
		entries := module.entryPoints(entryIDs)
		result.EntryPoints = append(result.EntryPoints, entries...)
		switch {
		case len(entries) > 0:
			module.layer = "controller"
		case module.isRepository():
			module.layer = "repository"
		case module.matchesNames(luaServiceNames):
			module.layer = "service"
		}
	}

	// What a controller reaches for is this project's service layer, unless it
	// already said it was something else. Spring says this with @Service and
	// Lua has no way to say it at all, so the graph's own shape is the only
	// evidence there is — and a module diagram where everything below the
	// entry point is one undifferentiated grey is a diagram nobody reads.
	for _, module := range modules {
		if module.layer != "controller" {
			continue
		}
		for _, req := range module.requires {
			if req.target != nil && req.target.layer == "unknown" {
				req.target.layer = "service"
			}
		}
	}
}

// entryPoints is every entry point one file declares.
func (m *luaModule) entryPoints(used map[string]bool) []EntryPoint {
	var found []EntryPoint
	add := func(ep EntryPoint) {
		ep.ID = luaEntryPointID(ep.Verb+" "+ep.Target, m.dotted, used)
		ep.Owner = m.dotted
		found = append(found, ep)
	}

	// A Kong plugin: phase functions on the table the file returns.
	if m.isKongHandler() {
		name := m.kongPluginName()
		for i, phase := range kongPhases {
			if fn := m.exports[phase]; fn != nil {
				// The phase is the verb and the plugin is the target. A Kong
				// plugin serves no route of its own — it runs inside every
				// route — so "ACCESS · rate-limiting" is the true shape of its
				// entry point, and inventing a URL for it would not be. The
				// phases read in the order Kong runs them.
				add(EntryPoint{
					Verb: strings.ToUpper(phase), Target: name, EntryNode: fn.nodeID,
					Kind: EntryKindPlugin, Group: name, Order: i + 1,
				})
			}
		}
	}

	// Kong's Admin API: a table keyed by route, each holding one function per
	// HTTP method. It belongs to the plugin whose api.lua it is.
	for _, fn := range m.order {
		route, httpMethod, ok := luaRouteFromDecl(fn.decl)
		if !ok {
			continue
		}
		add(EntryPoint{Verb: httpMethod, Target: route, EntryNode: fn.nodeID, Kind: EntryKindHTTP, Group: m.kongPluginName()})
	}

	// Lapis: routes declared as calls on the app.
	for _, ep := range m.lapisEntryPoints() {
		add(ep)
	}
	return found
}

// isKongHandler reports whether this file is a plugin's handler.
//
// Two signals, and it needs both: it exports at least one phase function, and
// it is recognisable as a handler — by the name Kong requires (handler.lua) or
// by the PRIORITY/VERSION fields every plugin sets. The pair matters because
// each alone is common and wrong: any module may have a function called `log`,
// and a schema.lua sits right beside a handler with neither.
func (m *luaModule) isKongHandler() bool {
	hasPhase := false
	for _, phase := range kongPhases {
		if m.exports[phase] != nil {
			hasPhase = true
			break
		}
	}
	if !hasPhase {
		return false
	}
	if path.Base(m.rel) == "handler.lua" {
		return true
	}
	return m.declaresField("PRIORITY") || m.declaresField("VERSION")
}

// declaresField reports whether the file sets a named field on a table at its
// top level — `M.PRIORITY = 900` or `local M = { PRIORITY = 900 }`.
func (m *luaModule) declaresField(name string) bool {
	found := false
	for _, stmt := range m.chunk.Block {
		lua.Walk(stmt, func(node lua.Node) bool {
			if found {
				return false
			}
			switch n := node.(type) {
			case *lua.FuncExpr:
				// Inside a function body it would be a runtime assignment, not
				// a declaration of what this plugin is.
				return false
			case *lua.TableExpr:
				for _, field := range n.Fields {
					if key, ok := luaFieldKey(field); ok && key == name {
						found = true
						return false
					}
				}
			case *lua.AssignStmt:
				for _, target := range n.Targets {
					if dotted, ok := luaDottedName(target); ok && strings.HasSuffix(dotted, "."+name) {
						found = true
						return false
					}
				}
			}
			return true
		})
	}
	return found
}

// kongPluginName is the name Kong knows this plugin by: the directory under
// kong/plugins/, which is the only place Kong looks. Failing that — a plugin
// repository checked out at its own root — the repository's own folder name,
// and failing that the module path, so the endpoint always says something.
func (m *luaModule) kongPluginName() string {
	segments := strings.Split(m.rel, "/")
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == "plugins" {
			return segments[i+1]
		}
	}
	if len(segments) > 1 {
		return segments[len(segments)-2]
	}
	return m.dotted
}

// luaRouteFromDecl reads Kong's Admin API shape out of a declaration path:
// `["/consumers/:id/keys"].GET` was written as a table keyed by route holding
// one function per method.
func luaRouteFromDecl(decl string) (routePath, httpMethod string, ok bool) {
	open := strings.LastIndex(decl, "[\"")
	if open < 0 {
		return "", "", false
	}
	rest := decl[open+2:]
	close := strings.Index(rest, "\"]")
	if close < 0 {
		return "", "", false
	}
	routePath = rest[:close]
	tail := rest[close+2:]
	if !strings.HasPrefix(routePath, "/") || !strings.HasPrefix(tail, ".") {
		return "", "", false
	}
	httpMethod = tail[1:]
	if !luaHTTPMethods[httpMethod] {
		return "", "", false
	}
	return routePath, httpMethod, true
}

// lapisEntryPoints reads routes declared as calls on a Lapis app: `app:get("/x",
// handler)`, with the optional route name Lapis allows in front of the path.
//
// The entry node is the handler when it names a function this file declared,
// and the module itself when it is the anonymous function it usually is —
// which is not a fudge: a closure has no node, and the calls inside it are
// already attributed to the module, so a flow starting there walks the right
// body.
func (m *luaModule) lapisEntryPoints() []EntryPoint {
	var found []EntryPoint
	for _, stmt := range m.chunk.Block {
		lua.Walk(stmt, func(node lua.Node) bool {
			call, ok := node.(*lua.MethodCallExpr)
			if !ok {
				return true
			}
			httpMethod, isRoute := lapisRouteMethods[call.Method]
			if !isRoute || len(call.Args) < 2 {
				return true
			}
			routePath := ""
			for _, arg := range call.Args {
				literal, isString := arg.(*lua.StringExpr)
				if isString && strings.HasPrefix(literal.Value, "/") {
					routePath = literal.Value
					break
				}
			}
			if routePath == "" {
				return true
			}
			entry := m.nodeID
			if name, isName := call.Args[len(call.Args)-1].(*lua.NameExpr); isName {
				if fn := m.funcs[name.Name]; fn != nil {
					entry = fn.nodeID
				}
			}
			found = append(found, EntryPoint{
				Verb: httpMethod, Target: routePath, EntryNode: entry, Kind: EntryKindHTTP, Group: m.dotted,
			})
			return true
		})
	}
	return found
}

// luaEntryPointID mirrors the Java analyzer's: "<METHOD> <path>", qualified with
// the module when two files claim the same route rather than dropping the
// second. Nothing parses the result; it only has to be stable and unique.
func luaEntryPointID(base, module string, used map[string]bool) string {
	if !used[base] {
		used[base] = true
		return base
	}
	qualified := base + " [" + module + "]"
	if !used[qualified] {
		used[qualified] = true
		return qualified
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s [%s #%d]", base, module, n)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}

// isRepository reports whether this file is where data is reached: it requires
// a database or cache client, or it is named like the module that wraps one.
func (m *luaModule) isRepository() bool {
	for _, req := range m.requires {
		for _, prefix := range luaDataAccessModules {
			if req.path == prefix || strings.HasPrefix(req.path, prefix+".") {
				return true
			}
		}
	}
	return m.matchesNames(luaRepositoryNames)
}

// matchesNames reports whether any segment of the module's path is one of the
// given words — "kong.plugins.x.daos.keys" is a repository because of `daos`,
// wherever in the path it sits.
func (m *luaModule) matchesNames(names map[string]bool) bool {
	for _, segment := range strings.Split(m.dotted, ".") {
		if names[strings.ToLower(segment)] {
			return true
		}
	}
	return false
}

// sortLuaEntryPoints keeps the index's entry points stable across runs. The
// analyzer walks files in path order already; this is the belt to that
// suspenders, since a map is involved in deciding which ones exist.
func sortLuaEntryPoints(entries []EntryPoint) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
}
