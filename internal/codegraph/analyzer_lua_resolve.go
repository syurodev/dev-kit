// internal/codegraph/analyzer_lua_resolve.go
package codegraph

import (
	"strings"

	"devkit/internal/lua"
)

// The resolution pass: which call goes where.
//
// Lua has no types, so this cannot work the way the Java analyzer does. What
// it has instead is provenance: a value is followable when it can be traced
// back to something declared — a `require` of a file in this project, the
// table this module returns, or the `self` of a method. A call made on such a
// value resolves to a node; every other call is recorded as a call site with a
// reason and no target.
//
// The reason is the point. "library" means the callee is outside the project
// (the standard library, ngx, kong, a required module with no file here) and
// there was never going to be a node for it. "unresolved" means the analyzer
// lost the value — a parameter, a field, a table built at run time. The two
// read very differently in an inspector: one is a wall, the other is a gap.

// luaValueKind is what an analyzer knows about a value's origin.
type luaValueKind int

const (
	luaUnknownValue luaValueKind = iota
	// luaModuleValue is a module of this project, reached through require.
	luaModuleValue
	// luaLibraryValue is something outside the project: a known global, or a
	// require that named no file here.
	luaLibraryValue
	// luaTableValue is a table declared in this same file — the module's own
	// table, or any other local one it declares functions on.
	luaTableValue
)

type luaValue struct {
	kind   luaValueKind
	module *luaModule
	lib    string
	table  string
}

// luaScope is a chain of lexical scopes. Shadowing matters: a `local kong =
// kong` at the top of a file and a `local kong` inside one function are
// different values, and following the second as if it were the first would
// resolve calls to the wrong place.
type luaScope struct {
	parent *luaScope
	vars   map[string]luaValue
}

func newLuaScope(parent *luaScope) *luaScope {
	return &luaScope{parent: parent, vars: map[string]luaValue{}}
}

func (s *luaScope) get(name string) (luaValue, bool) {
	for scope := s; scope != nil; scope = scope.parent {
		if value, ok := scope.vars[name]; ok {
			return value, true
		}
	}
	return luaValue{}, false
}

func (s *luaScope) set(name string, value luaValue) { s.vars[name] = value }

// luaKnownGlobals is the standard library plus the globals every OpenResty and
// Kong file is written against. A call rooted at one of these is a wall, not a
// gap: naming it "library" is what stops the inspector from reporting half a
// Kong plugin as unresolved.
var luaKnownGlobals = map[string]bool{
	// Lua 5.1–5.4 base library and modules.
	"_G": true, "_ENV": true, "assert": true, "collectgarbage": true, "dofile": true,
	"error": true, "getfenv": true, "getmetatable": true, "ipairs": true, "load": true,
	"loadfile": true, "loadstring": true, "module": true, "next": true, "pairs": true,
	"pcall": true, "print": true, "rawequal": true, "rawget": true, "rawlen": true,
	"rawset": true, "require": true, "select": true, "setfenv": true, "setmetatable": true,
	"tonumber": true, "tostring": true, "type": true, "unpack": true, "xpcall": true,
	"coroutine": true, "debug": true, "io": true, "math": true, "os": true,
	"package": true, "string": true, "table": true, "utf8": true, "arg": true,
	// LuaJIT.
	"bit": true, "jit": true, "ffi": true, "newproxy": true,
	// OpenResty and Kong, which are globals in every file that runs inside them.
	"ngx": true, "kong": true,
	// busted / luassert, so a spec file's calls read as library rather than as
	// a project full of unresolved calls.
	"describe": true, "it": true, "before_each": true, "after_each": true,
	"setup": true, "teardown": true, "lazy_setup": true, "lazy_teardown": true,
	"spec": true, "expose": true, "insulate": true, "randomize": true,
	"pending": true, "finally": true, "stub": true, "mock": true, "spy": true,
}

// luaConstructors are the method names that hand back an instance of the thing
// they were called on. Following a module through one of these is what makes
// the standard Lua OOP idiom resolvable — `local Obj = require "x"; local o =
// Obj:new(); o:run()` — without following it through every other call, where
// the return value is data rather than an instance and the edge would be a
// fabrication.
var luaConstructors = map[string]bool{
	"new": true, "create": true, "init": true, "extend": true, "instance": true,
	"open": true, "spawn": true,
}

// luaResolver resolves one enclosing node's body.
type luaResolver struct {
	module *luaModule
	result *AnalysisResult
	// from is the node every call site and edge is attributed to: the function
	// being walked, or the module itself for code at the top level of a file.
	from string
	// edges dedupes call edges. The graph wants one edge per relationship; the
	// call sites keep every individual call.
	edges map[string]bool
}

// resolveCalls walks the whole file: its top level under the module node, then
// each declared function under its own.
func (m *luaModule) resolveCalls(index *luaIndex, result *AnalysisResult) {
	edges := map[string]bool{}
	base := m.moduleScope()

	top := &luaResolver{module: m, result: result, from: m.nodeID, edges: edges}
	top.walkTopLevel(m.chunk.Block, newLuaScope(base))

	for _, fn := range m.order {
		if fn.body == nil {
			continue
		}
		scope := newLuaScope(base)
		for _, param := range fn.body.Params {
			// A parameter shadows whatever the module level bound, and the
			// analyzer knows nothing about what was passed in.
			scope.set(param, luaValue{})
		}
		if table := fn.selfBinding(); table != "" {
			scope.set("self", luaValue{kind: luaTableValue, table: table})
		}
		resolver := &luaResolver{module: m, result: result, from: fn.nodeID, edges: edges}
		resolver.walkBlock(fn.body.Body, scope)
	}
}

// selfBinding is the table this function's `self` refers to.
//
// The colon form says so outright. The dot form with an explicit `self` first
// parameter — `function Store.count(self)` — means exactly the same thing and
// is how plenty of Lua is written; without this, every call through that self
// would be a gap.
func (fn *luaFunc) selfBinding() string {
	if fn.selfTable != "" {
		return fn.selfTable
	}
	if len(fn.params) == 0 || fn.params[0].Name != "self" {
		return ""
	}
	table, _, found := strings.Cut(fn.decl, ".")
	if !found {
		return ""
	}
	return table
}

// moduleScope is what every function in the file starts from: the modules it
// required, the tables it declares functions on, and the table it returns.
func (m *luaModule) moduleScope() *luaScope {
	scope := newLuaScope(nil)
	for _, req := range m.requires {
		if req.name == "" {
			continue
		}
		if req.target != nil {
			scope.set(req.name, luaValue{kind: luaModuleValue, module: req.target})
			continue
		}
		scope.set(req.name, luaValue{kind: luaLibraryValue, lib: req.path})
	}
	// Every table a declaration was hung on is a table of this file, the
	// module's own included. Binding them by name is what resolves `M.helper()`
	// and `Policies.redis(conf)` inside the same file.
	tables := map[string]bool{}
	for _, fn := range m.order {
		if prefix, _, found := strings.Cut(fn.decl, "."); found && prefix != "" {
			tables[prefix] = true
		}
		if fn.selfTable != "" {
			tables[fn.selfTable] = true
		}
	}
	// And the table the file returns, which a module with fields but no
	// functions on it has no declaration to reveal.
	if m.tableVar != "" {
		tables[m.tableVar] = true
	}
	for table := range tables {
		if _, taken := scope.get(table); taken {
			// A required module wins: the file said where that name came from.
			continue
		}
		scope.set(table, luaValue{kind: luaTableValue, table: table})
	}
	return scope
}

// walkTopLevel walks a file's own statements without descending into the
// function bodies that hang off them — those are walked separately, under
// their own node.
//
// It recurses through `do`, `if`, `for` and `while` itself rather than handing
// them to walkStmt, and that is not tidiness. Conditional declaration is
// idiomatic in Lua (`if ngx.config.subsystem == "http" then function M.access()
// ... end`), the declaration pass already gave those functions nodes of their
// own, and walkStmt would walk their bodies again — every call inside them
// recorded a second time, once against the function and once against the
// module.
func (r *luaResolver) walkTopLevel(block []lua.Stmt, scope *luaScope) {
	for _, stmt := range block {
		switch s := stmt.(type) {
		case *lua.FuncStmt, *lua.LocalFuncStmt:
			// Declaration only; the body is somebody else's node.
		case *lua.LocalStmt:
			// Right-hand side first: `local kong = kong` reads the outer one.
			r.walkExprsSkippingFunctions(s.Exprs, scope)
			r.bindLocals(s.Names, s.Exprs, scope)
		case *lua.AssignStmt:
			r.walkAssign(s, scope, true)
		case *lua.DoStmt:
			r.walkTopLevel(s.Body, newLuaScope(scope))
		case *lua.WhileStmt:
			r.walkExpr(s.Cond, scope)
			r.walkTopLevel(s.Body, newLuaScope(scope))
		case *lua.RepeatStmt:
			inner := newLuaScope(scope)
			r.walkTopLevel(s.Body, inner)
			r.walkExpr(s.Cond, inner)
		case *lua.IfStmt:
			for _, clause := range s.Clauses {
				r.walkExpr(clause.Cond, scope)
				r.walkTopLevel(clause.Body, newLuaScope(scope))
			}
			r.walkTopLevel(s.Else, newLuaScope(scope))
		case *lua.NumericForStmt:
			r.walkExpr(s.Start, scope)
			r.walkExpr(s.Limit, scope)
			r.walkExpr(s.Step, scope)
			inner := newLuaScope(scope)
			inner.set(s.Name, luaValue{})
			r.walkTopLevel(s.Body, inner)
		case *lua.GenericForStmt:
			r.walkExprs(s.Exprs, scope)
			inner := newLuaScope(scope)
			for _, name := range s.Names {
				inner.set(name, luaValue{})
			}
			r.walkTopLevel(s.Body, inner)
		default:
			r.walkStmt(stmt, scope)
		}
	}
}

func (r *luaResolver) walkBlock(block []lua.Stmt, scope *luaScope) {
	for _, stmt := range block {
		r.walkStmt(stmt, scope)
	}
}

func (r *luaResolver) walkStmt(stmt lua.Stmt, scope *luaScope) {
	switch s := stmt.(type) {
	case *lua.LocalStmt:
		r.walkExprs(s.Exprs, scope)
		r.bindLocals(s.Names, s.Exprs, scope)
	case *lua.LocalFuncStmt:
		scope.set(s.Name, luaValue{})
		if s.Func != nil {
			r.walkBlock(s.Func.Body, newLuaScope(scope))
		}
	case *lua.FuncStmt:
		if s.Func != nil {
			inner := newLuaScope(scope)
			if s.Method != "" {
				inner.set("self", luaValue{kind: luaTableValue, table: strings.Join(s.Path, ".")})
			}
			r.walkBlock(s.Func.Body, inner)
		}
	case *lua.AssignStmt:
		r.walkAssign(s, scope, false)
	case *lua.CallStmt:
		r.walkExpr(s.Call, scope)
	case *lua.DoStmt:
		r.walkBlock(s.Body, newLuaScope(scope))
	case *lua.WhileStmt:
		r.walkExpr(s.Cond, scope)
		r.walkBlock(s.Body, newLuaScope(scope))
	case *lua.RepeatStmt:
		inner := newLuaScope(scope)
		r.walkBlock(s.Body, inner)
		// `until` sees the body's locals, which is the one place Lua's scoping
		// is not what a reader expects.
		r.walkExpr(s.Cond, inner)
	case *lua.IfStmt:
		for _, clause := range s.Clauses {
			r.walkExpr(clause.Cond, scope)
			r.walkBlock(clause.Body, newLuaScope(scope))
		}
		r.walkBlock(s.Else, newLuaScope(scope))
	case *lua.NumericForStmt:
		r.walkExpr(s.Start, scope)
		r.walkExpr(s.Limit, scope)
		r.walkExpr(s.Step, scope)
		inner := newLuaScope(scope)
		inner.set(s.Name, luaValue{})
		r.walkBlock(s.Body, inner)
	case *lua.GenericForStmt:
		r.walkExprs(s.Exprs, scope)
		inner := newLuaScope(scope)
		for _, name := range s.Names {
			inner.set(name, luaValue{})
		}
		r.walkBlock(s.Body, inner)
	case *lua.ReturnStmt:
		r.walkExprs(s.Exprs, scope)
	}
}

// walkAssign records the writes an assignment performs, then walks both sides.
// topLevel keeps a module's own `function M.f = ...` bodies out of the module
// node's walk, since they already have nodes of their own.
func (r *luaResolver) walkAssign(s *lua.AssignStmt, scope *luaScope, topLevel bool) {
	for _, target := range s.Targets {
		index, ok := target.(*lua.IndexExpr)
		if !ok {
			continue
		}
		r.fieldAccess(index, scope, "write")
		r.walkExpr(index.Obj, scope)
		if !index.Dot {
			r.walkExpr(index.Key, scope)
		}
	}
	if topLevel {
		r.walkExprsSkippingFunctions(s.Exprs, scope)
		return
	}
	r.walkExprs(s.Exprs, scope)
}

// walkExprsSkippingFunctions walks a top-level expression list but leaves
// function literals alone — a `M.f = function() ... end` body belongs to M.f's
// node, not to the module's.
func (r *luaResolver) walkExprsSkippingFunctions(exprs []lua.Expr, scope *luaScope) {
	for _, expr := range exprs {
		switch value := expr.(type) {
		case *lua.FuncExpr:
			continue
		case *lua.TableExpr:
			for _, field := range value.Fields {
				if _, isFunc := field.Value.(*lua.FuncExpr); isFunc {
					continue
				}
				r.walkExpr(field.Value, scope)
			}
		default:
			r.walkExpr(expr, scope)
		}
	}
}

func (r *luaResolver) walkExprs(exprs []lua.Expr, scope *luaScope) {
	for _, expr := range exprs {
		r.walkExpr(expr, scope)
	}
}

func (r *luaResolver) walkExpr(expr lua.Expr, scope *luaScope) {
	switch e := expr.(type) {
	case nil:
		return
	case *lua.CallExpr:
		r.recordCall(e.Func, nil, e.Line, scope)
		r.walkCallee(e.Func, scope)
		r.walkExprs(e.Args, scope)
	case *lua.MethodCallExpr:
		r.recordCall(e.Recv, &e.Method, e.Line, scope)
		r.walkExpr(e.Recv, scope)
		r.walkExprs(e.Args, scope)
	case *lua.IndexExpr:
		r.fieldAccess(e, scope, "read")
		r.walkExpr(e.Obj, scope)
		if !e.Dot {
			r.walkExpr(e.Key, scope)
		}
	case *lua.FuncExpr:
		// A closure's calls belong to the function that declares it: the
		// closure has no node, and attributing them to nothing would lose
		// them. A child scope, because its parameters shadow.
		inner := newLuaScope(scope)
		for _, param := range e.Params {
			inner.set(param, luaValue{})
		}
		r.walkBlock(e.Body, inner)
	case *lua.BinOpExpr:
		r.walkExpr(e.Left, scope)
		r.walkExpr(e.Right, scope)
	case *lua.UnOpExpr:
		r.walkExpr(e.Operand, scope)
	case *lua.TableExpr:
		for _, field := range e.Fields {
			if field.Name == "" && field.Key != nil {
				r.walkExpr(field.Key, scope)
			}
			r.walkExpr(field.Value, scope)
		}
	}
}

// walkCallee walks what a call was made ON, without treating the callee's own
// name as a field read — `M.helper()` is a call to helper, not a read of a
// field called helper.
func (r *luaResolver) walkCallee(callee lua.Expr, scope *luaScope) {
	switch e := callee.(type) {
	case *lua.NameExpr:
		return
	case *lua.IndexExpr:
		r.walkExpr(e.Obj, scope)
		if !e.Dot {
			r.walkExpr(e.Key, scope)
		}
	default:
		r.walkExpr(callee, scope)
	}
}

// recordCall resolves one call and records it. method is non-nil for the colon
// form, where callee is the receiver rather than the function.
func (r *luaResolver) recordCall(callee lua.Expr, method *string, line int, scope *luaScope) {
	target, reason := r.resolve(callee, method, scope)
	site := CallSite{From: r.from, Line: line}
	if target != nil {
		site.To = target.nodeID
	} else {
		site.Unresolved = reason
	}
	r.result.CallSites = append(r.result.CallSites, site)

	if target == nil {
		return
	}
	key := r.from + "\x00" + target.nodeID
	if r.edges[key] {
		return
	}
	r.edges[key] = true
	r.result.Edges = append(r.result.Edges, Edge{From: r.from, To: target.nodeID, Kind: "call"})
}

// resolve answers what a call reaches, or why it could not be followed.
func (r *luaResolver) resolve(callee lua.Expr, method *string, scope *luaScope) (*luaFunc, string) {
	if method != nil {
		return r.resolveOn(r.eval(callee, scope), callee, *method, scope)
	}
	switch e := callee.(type) {
	case *lua.NameExpr:
		if fn := r.module.funcs[e.Name]; fn != nil {
			return fn, ""
		}
		if value, bound := scope.get(e.Name); bound {
			switch value.kind {
			case luaLibraryValue:
				return nil, "library"
			case luaModuleValue:
				// The file required a module and is calling it: the module
				// returns a function rather than a table. Nothing names which
				// declaration that is, so it stays a gap rather than a guess.
				return nil, "unresolved"
			}
			return nil, "unresolved"
		}
		if luaKnownGlobals[e.Name] {
			return nil, "library"
		}
		return nil, "unresolved"

	case *lua.IndexExpr:
		key, isString := e.Key.(*lua.StringExpr)
		if !isString {
			return nil, "unresolved"
		}
		return r.resolveOn(r.eval(e.Obj, scope), e.Obj, key.Value, scope)
	}
	return nil, "unresolved"
}

// resolveOn resolves `<base>.<name>` or `<base>:<name>` given what the base
// evaluated to.
func (r *luaResolver) resolveOn(base luaValue, baseExpr lua.Expr, name string, scope *luaScope) (*luaFunc, string) {
	switch base.kind {
	case luaModuleValue:
		if fn := base.module.exports[name]; fn != nil {
			return fn, ""
		}
		// A module of this project that does not export this name: either the
		// call is wrong or the declaration is one this analyzer could not see.
		// Either way there is no node to point at.
		return nil, "unresolved"
	case luaLibraryValue:
		return nil, "library"
	case luaTableValue:
		if fn := r.module.funcs[luaJoinField(base.table, name)]; fn != nil {
			return fn, ""
		}
		return nil, "unresolved"
	}

	// Nothing known about the base. Two last chances, both about names the
	// file itself declared: the whole dotted path, and a root that is a global
	// the project has never heard of.
	if dotted, ok := luaDottedName(baseExpr); ok {
		if fn := r.module.funcs[luaJoinField(dotted, name)]; fn != nil {
			return fn, ""
		}
		root, _, _ := strings.Cut(dotted, ".")
		if luaKnownGlobals[root] {
			return nil, "library"
		}
	}
	return nil, "unresolved"
}

// eval works out what an expression's value came from.
func (r *luaResolver) eval(expr lua.Expr, scope *luaScope) luaValue {
	switch e := expr.(type) {
	case *lua.NameExpr:
		if value, ok := scope.get(e.Name); ok {
			return value
		}
		if luaKnownGlobals[e.Name] {
			return luaValue{kind: luaLibraryValue, lib: e.Name}
		}
		return luaValue{}

	case *lua.IndexExpr:
		base := r.eval(e.Obj, scope)
		if base.kind == luaLibraryValue {
			// ngx.timer, kong.db: still outside the project.
			return base
		}
		return luaValue{}

	case *lua.CallExpr:
		if path, ok := luaRequireCall(e); ok {
			return r.valueForRequire(path)
		}
		if index, isIndex := e.Func.(*lua.IndexExpr); isIndex {
			if key, isString := index.Key.(*lua.StringExpr); isString {
				return r.throughConstructor(r.eval(index.Obj, scope), key.Value)
			}
		}
		return luaValue{}

	case *lua.MethodCallExpr:
		if path, ok := luaRequireCall(e); ok {
			return r.valueForRequire(path)
		}
		return r.throughConstructor(r.eval(e.Recv, scope), e.Method)
	}
	return luaValue{}
}

// valueForRequire is what a `require` in an expression position evaluates to.
func (r *luaResolver) valueForRequire(path string) luaValue {
	for _, req := range r.module.requires {
		if req.path != path {
			continue
		}
		if req.target != nil {
			return luaValue{kind: luaModuleValue, module: req.target}
		}
		return luaValue{kind: luaLibraryValue, lib: path}
	}
	return luaValue{kind: luaLibraryValue, lib: path}
}

// throughConstructor decides whether a call's result still carries its
// receiver's origin.
//
// A library's does, always: whatever `redis:new()` or `cjson.decode()` hands
// back is still not in this project, and saying so costs nothing — there is no
// node it could have resolved to either way. A project module's does not,
// unless the call is one of the constructor names: the return value of an
// ordinary method is data, and treating it as an instance of the module would
// resolve the next call to a function that never runs.
func (r *luaResolver) throughConstructor(base luaValue, method string) luaValue {
	switch base.kind {
	case luaLibraryValue:
		return base
	case luaModuleValue, luaTableValue:
		if luaConstructors[method] {
			return base
		}
	}
	return luaValue{}
}

// fieldAccess records a read or a write of a field on a table this file owns —
// `self.conf`, `M.PRIORITY`. Fields are where a Lua module keeps its state, and
// they are what the data-flow trace follows.
//
// Only tables of this file: a field on a required module is that module's, and
// recording it here would attribute it to the wrong class. And never a name
// that is a declared function — `M.access` is a function, not a field, and
// listing it among the fields would bury the real ones.
func (r *luaResolver) fieldAccess(index *lua.IndexExpr, scope *luaScope, kind string) {
	key, isString := index.Key.(*lua.StringExpr)
	if !isString {
		return
	}
	name, isName := index.Obj.(*lua.NameExpr)
	if !isName {
		return
	}
	value, bound := scope.get(name.Name)
	if !bound || value.kind != luaTableValue {
		return
	}
	if r.module.funcs[luaJoinField(value.table, key.Value)] != nil {
		return
	}
	r.result.FieldAccesses = append(r.result.FieldAccesses, FieldAccess{
		ClassID:   r.module.nodeID,
		NodeID:    r.from,
		FieldName: key.Value,
		Kind:      kind,
		Line:      index.Line,
	})
}

// bindLocals binds the names a `local` statement introduces to whatever the
// analyzer could work out about the values beside them.
func (r *luaResolver) bindLocals(names []string, exprs []lua.Expr, scope *luaScope) {
	for i, name := range names {
		if i >= len(exprs) {
			scope.set(name, luaValue{})
			continue
		}
		value := r.eval(exprs[i], scope)
		if _, isTable := exprs[i].(*lua.TableExpr); isTable {
			// A fresh table declared here is a table of this file, which is
			// what makes `local Policies = {}` followed by
			// `function Policies.redis()` resolvable.
			value = luaValue{kind: luaTableValue, table: name}
		}
		scope.set(name, value)
	}
}
