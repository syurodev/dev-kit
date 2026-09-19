// internal/codegraph/analyzer_lua_declare.go
package codegraph

import (
	"strings"

	"devkit/internal/lua"
)

// The declaration pass: what each file declares, and which of it another file
// can reach.
//
// Lua has no export keyword. What a module offers is whatever ends up on the
// table it returns, and there are half a dozen ways to put a function there —
// `function M.f()`, `function M:f()`, `M.f = function()`, `local M = { f =
// function() end }`, `return { f = function() end }`. All of them are here,
// because a module written in one style and called from a file written in
// another is the normal case, and an analyzer that read only the first style
// would resolve a project's calls at random.

// declare finds every function and every require in one file, then works out
// which of those functions the rest of the project can call.
func (m *luaModule) declare(index *luaIndex) {
	m.collect(m.chunk.Block)
	m.tableVar = m.moduleTableVar()
	m.assignIDs(index)
	// Every doc comment has been read; nothing below this point looks at the
	// source text again.
	m.lines = nil
}

// collect walks a block for declarations. It descends into `do`, `if`, `for`
// and `while` bodies — conditional declaration is idiomatic in Lua
// (`if ngx.config.subsystem == "http" then function M.access() ...`) — but
// never into a function body: a function declared inside another is a closure,
// not part of the module's surface, and giving it a node of its own would put
// a symbol in the index that no caller can name.
func (m *luaModule) collect(block []lua.Stmt) {
	for _, stmt := range block {
		switch s := stmt.(type) {
		case *lua.FuncStmt:
			decl := strings.Join(s.Path, ".")
			selfTable := ""
			if s.Method != "" {
				selfTable = decl
				decl += "." + s.Method
			}
			m.addFunc(decl, s.Func, selfTable, len(s.Path) == 1 && s.Method == "")

		case *lua.LocalFuncStmt:
			m.addFunc(s.Name, s.Func, "", false)

		case *lua.LocalStmt:
			for i, name := range s.Names {
				if i >= len(s.Exprs) {
					break
				}
				switch value := s.Exprs[i].(type) {
				case *lua.FuncExpr:
					m.addFunc(name, value, "", false)
				case *lua.TableExpr:
					m.declareTableFuncs(name, value)
				}
			}
			m.collectRequires(s.Names, s.Exprs)

		case *lua.AssignStmt:
			var names []string
			for _, target := range s.Targets {
				dotted, _ := luaDottedName(target)
				names = append(names, dotted)
			}
			for i, dotted := range names {
				if dotted == "" || i >= len(s.Exprs) {
					continue
				}
				switch value := s.Exprs[i].(type) {
				case *lua.FuncExpr:
					m.addFunc(dotted, value, "", !strings.Contains(dotted, "."))
				case *lua.TableExpr:
					m.declareTableFuncs(dotted, value)
				}
			}
			m.collectRequires(names, s.Exprs)

		case *lua.CallStmt:
			// A require whose result is thrown away is still a dependency:
			// `require "kong.plugins.x.migrations"` is how a module is pulled
			// in for its side effects.
			if path, ok := luaRequireCall(s.Call); ok {
				m.requires = append(m.requires, luaRequire{path: path, line: s.Line})
			}

		case *lua.ReturnStmt:
			for _, expr := range s.Exprs {
				if table, ok := expr.(*lua.TableExpr); ok {
					m.declareTableFuncs("", table)
				}
			}

		case *lua.DoStmt:
			m.collect(s.Body)
		case *lua.WhileStmt:
			m.collect(s.Body)
		case *lua.RepeatStmt:
			m.collect(s.Body)
		case *lua.NumericForStmt:
			m.collect(s.Body)
		case *lua.GenericForStmt:
			m.collect(s.Body)
		case *lua.IfStmt:
			for _, clause := range s.Clauses {
				m.collect(clause.Body)
			}
			m.collect(s.Else)
		}
	}
}

// declareTableFuncs reads functions out of a table constructor, at any depth.
// Depth is what Kong's Admin API needs: api.lua returns
// `{ ["/status"] = { GET = function() end } }`, and the function worth a node
// is two levels in.
func (m *luaModule) declareTableFuncs(prefix string, table *lua.TableExpr) {
	for _, field := range table.Fields {
		key, ok := luaFieldKey(field)
		if !ok {
			continue
		}
		name := luaJoinField(prefix, key)
		switch value := field.Value.(type) {
		case *lua.FuncExpr:
			m.addFunc(name, value, "", false)
		case *lua.TableExpr:
			m.declareTableFuncs(name, value)
		}
	}
}

// luaFieldKey is a table field's key when it has a static one — `name =` and
// `["name"] =` both count, `[i] =` and a positional item do not.
func luaFieldKey(field lua.TableField) (string, bool) {
	if field.Name != "" {
		return field.Name, true
	}
	if key, ok := field.Key.(*lua.StringExpr); ok {
		return key.Value, true
	}
	return "", false
}

// luaJoinField appends one key to a dotted path, bracketing a key that is not
// an identifier so the result still reads as Lua: api["/status"].GET.
func luaJoinField(prefix, key string) string {
	segment := key
	if !isLuaIdentifier(key) {
		segment = "[\"" + key + "\"]"
	}
	switch {
	case prefix == "":
		return segment
	case strings.HasPrefix(segment, "["):
		return prefix + segment
	default:
		return prefix + "." + segment
	}
}

func isLuaIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if letter || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// addFunc records one declaration. First declaration wins for a repeated name:
// the same function being assigned twice is one function to a reader, and the
// first is where it was written.
func (m *luaModule) addFunc(decl string, body *lua.FuncExpr, selfTable string, global bool) {
	if decl == "" || body == nil || m.funcs[decl] != nil {
		return
	}
	fn := &luaFunc{
		decl:      decl,
		line:      body.Line,
		endLine:   body.EndLine,
		body:      body,
		selfTable: selfTable,
		summary:   luaCommentAbove(m.lines, body.Line),
		global:    global,
	}
	for _, param := range body.Params {
		// No type: Lua has none, and inventing one ("any") would put a word on
		// the screen that says nothing.
		fn.params = append(fn.params, Param{Name: param})
	}
	if body.IsVararg {
		fn.params = append(fn.params, Param{Name: "..."})
	}
	m.funcs[decl] = fn
	m.order = append(m.order, fn)
}

// collectRequires pairs `local name = require "path"` across a whole
// assignment, so `local a, b = require "x", require "y"` binds both.
func (m *luaModule) collectRequires(names []string, exprs []lua.Expr) {
	for i, expr := range exprs {
		path, ok := luaRequireCall(expr)
		if !ok {
			continue
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		// Only a plain name binds: `M.redis = require "resty.redis"` is a
		// dependency, but the local variable it can be called through is
		// whatever the file indexes it back out with later.
		if strings.Contains(name, ".") || strings.Contains(name, "[") {
			name = ""
		}
		m.requires = append(m.requires, luaRequire{name: name, path: path, line: expr.Pos().Line})
	}
}

// luaRequireCall reports the path when an expression is a `require`.
//
// It sees through the two wrappers real code puts around one: `require("x").y`
// takes a field off the module, and `pcall(require, "x")` is how optional
// dependencies are loaded. Both are dependencies of this file either way.
func luaRequireCall(expr lua.Expr) (string, bool) {
	switch e := expr.(type) {
	case *lua.CallExpr:
		if name, ok := e.Func.(*lua.NameExpr); ok {
			if name.Name == "require" && len(e.Args) == 1 {
				if literal, isString := e.Args[0].(*lua.StringExpr); isString {
					return literal.Value, true
				}
			}
			if name.Name == "pcall" && len(e.Args) == 2 {
				if inner, isName := e.Args[0].(*lua.NameExpr); isName && inner.Name == "require" {
					if literal, isString := e.Args[1].(*lua.StringExpr); isString {
						return literal.Value, true
					}
				}
			}
			return "", false
		}
		// `require("cjson").new()`: the call is made ON a require, so the
		// require is under the callee rather than being it.
		return luaRequireCall(e.Func)
	case *lua.IndexExpr:
		return luaRequireCall(e.Obj)
	case *lua.MethodCallExpr:
		return luaRequireCall(e.Recv)
	}
	return "", false
}

// luaDottedName turns a target expression back into the path it was written
// as: `M.util.trim` from the index chain, "" for anything computed.
func luaDottedName(expr lua.Expr) (string, bool) {
	switch e := expr.(type) {
	case *lua.NameExpr:
		return e.Name, true
	case *lua.IndexExpr:
		prefix, ok := luaDottedName(e.Obj)
		if !ok {
			return "", false
		}
		key, isString := e.Key.(*lua.StringExpr)
		if !isString {
			return "", false
		}
		return luaJoinField(prefix, key.Value), true
	}
	return "", false
}

// moduleTableVar is the local name this file's module table is bound to.
//
// The return statement is the authority, because it is what `require` hands
// back — including through `setmetatable(M, mt)`, which is how a module with
// inheritance is returned. Failing that, the most common prefix among the
// declarations is the table: a file that declares M.a, M.b and M.c and forgot
// to return anything is still a module whose surface is those three.
func (m *luaModule) moduleTableVar() string {
	for i := len(m.chunk.Block) - 1; i >= 0; i-- {
		ret, ok := m.chunk.Block[i].(*lua.ReturnStmt)
		if !ok || len(ret.Exprs) == 0 {
			continue
		}
		if name := luaReturnedName(ret.Exprs[0]); name != "" {
			return name
		}
		break
	}

	counts := map[string]int{}
	best, bestCount := "", 0
	for _, fn := range m.order {
		prefix, _, found := strings.Cut(fn.decl, ".")
		// Only a plain variable can be the module table. A bracketed key —
		// Kong's api.lua is `{ ["/status"] = { GET = ... } }` — is part of
		// what the function is called, and stripping it would leave every
		// route's handler labelled just "GET".
		if !found || !isLuaIdentifier(prefix) {
			continue
		}
		counts[prefix]++
		// Ties go to the first declaration, which keeps the choice stable
		// across runs where map order is not.
		if counts[prefix] > bestCount {
			best, bestCount = prefix, counts[prefix]
		}
	}
	return best
}

// luaReturnedName is the variable a return statement hands back, seeing
// through the wrappers a module is conventionally returned in.
func luaReturnedName(expr lua.Expr) string {
	switch e := expr.(type) {
	case *lua.NameExpr:
		return e.Name
	case *lua.CallExpr:
		// return setmetatable(M, mt)
		for _, arg := range e.Args {
			if name, ok := arg.(*lua.NameExpr); ok {
				return name.Name
			}
		}
	}
	return ""
}

// assignIDs gives every declaration its node id, label and public name, once
// the module table is known.
//
// The label drops the module table's variable, because that name is local
// trivia: the file calls it M, its caller calls it whatever it likes, and the
// only stable name for the function is <module>.<what it is called>. The id
// adds the parameter list, the same way the Java analyzer's does, so two
// declarations that differ only in shape stay two nodes.
func (m *luaModule) assignIDs(index *luaIndex) {
	for _, fn := range m.order {
		display := fn.decl
		exported := fn.global
		if m.tableVar != "" {
			if rest := strings.TrimPrefix(fn.decl, m.tableVar+"."); rest != fn.decl {
				display, exported = rest, true
			}
		}
		// A function declared straight into the returned table literal has no
		// prefix to strip and is exported by construction.
		if !strings.Contains(fn.decl, ".") && m.declaredInReturnTable(fn.decl) {
			exported = true
		}

		fn.label = m.nodeID + "." + display
		if exported {
			fn.public = display
			m.exports[display] = fn
		}
		names := make([]string, 0, len(fn.params))
		for _, param := range fn.params {
			names = append(names, param.Name)
		}
		fn.nodeID = index.uniqueID(fn.label + "(" + strings.Join(names, ",") + ")")
	}
}

// declaredInReturnTable reports whether a bare name came from `return { name =
// function() end }` rather than from a `local function`.
func (m *luaModule) declaredInReturnTable(name string) bool {
	for _, stmt := range m.chunk.Block {
		ret, ok := stmt.(*lua.ReturnStmt)
		if !ok {
			continue
		}
		for _, expr := range ret.Exprs {
			table, isTable := expr.(*lua.TableExpr)
			if !isTable {
				continue
			}
			for _, field := range table.Fields {
				if key, hasKey := luaFieldKey(field); hasKey && key == name {
					return true
				}
			}
		}
	}
	return false
}
