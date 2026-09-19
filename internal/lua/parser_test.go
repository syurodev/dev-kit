// internal/lua/parser_test.go
package lua

import (
	"strings"
	"testing"
)

// parseClean parses source that is expected to have nothing wrong with it, and
// fails the test naming the first diagnostic if it does. Most tests below are
// really two assertions — "this shape parses at all" and "it parses into the
// right thing" — and the first is the one worth failing loudly.
func parseClean(t *testing.T, src string) *Chunk {
	t.Helper()
	chunk := Parse(src)
	if len(chunk.Diagnostics) > 0 {
		t.Fatalf("expected a clean parse, got %d diagnostics; first: line %d: %s",
			len(chunk.Diagnostics), chunk.Diagnostics[0].Line, chunk.Diagnostics[0].Message)
	}
	return chunk
}

func TestParsesAKongPluginHandler(t *testing.T) {
	chunk := parseClean(t, `
local kong = kong
local redis = require "resty.redis"

local RateLimit = {
  PRIORITY = 900,
  VERSION = "1.0.0",
}

function RateLimit:access(conf)
  local red = redis:new()
  red:connect(conf.host, conf.port)
  local hits = red:incr(conf.key)
  if hits > conf.limit then
    return kong.response.exit(429, { message = "too many requests" })
  end
end

return RateLimit
`)

	var fn *FuncStmt
	for _, stmt := range chunk.Block {
		if f, ok := stmt.(*FuncStmt); ok {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("expected a function statement for RateLimit:access")
	}
	if got := strings.Join(fn.Path, "."); got != "RateLimit" {
		t.Errorf("path = %q, want RateLimit", got)
	}
	if fn.Method != "access" {
		t.Errorf("method = %q, want access", fn.Method)
	}
	// The colon form takes self as its first parameter even though the source
	// never writes it — resolving `self:other()` depends on that.
	if len(fn.Func.Params) != 2 || fn.Func.Params[0] != "self" || fn.Func.Params[1] != "conf" {
		t.Errorf("params = %v, want [self conf]", fn.Func.Params)
	}
	if !fn.Func.Method {
		t.Error("expected the function body to be marked as a method")
	}
}

func TestRequireInEveryCallForm(t *testing.T) {
	chunk := parseClean(t, `
local a = require("one")
local b = require "two"
local c = require [[three]]
`)
	var required []string
	for _, stmt := range chunk.Block {
		Walk(stmt, func(node Node) bool {
			call, ok := node.(*CallExpr)
			if !ok {
				return true
			}
			name, isName := call.Func.(*NameExpr)
			if !isName || name.Name != "require" || len(call.Args) != 1 {
				return true
			}
			if lit, isString := call.Args[0].(*StringExpr); isString {
				required = append(required, lit.Value)
			}
			return true
		})
	}
	if got := strings.Join(required, ","); got != "one,two,three" {
		t.Errorf("required = %q, want one,two,three", got)
	}
}

func TestParsesLua54Syntax(t *testing.T) {
	// // and the bitwise operators are 5.4; <const>/<close> are 5.4; goto and
	// its labels are 5.2. A Kong tree holds all of it at once.
	chunk := parseClean(t, `
local x <const> = 7 // 2
local y = (x & 0xff) | (x ~ 1) | (x << 2) | (x >> 1)
for i = 1, 10 do
  if i == 3 then goto continue end
  y = y + i
  ::continue::
end
local f <close> = nil
return y
`)
	if len(chunk.Block) == 0 {
		t.Fatal("expected statements")
	}
	local, ok := chunk.Block[0].(*LocalStmt)
	if !ok {
		t.Fatalf("first statement is %T, want *LocalStmt", chunk.Block[0])
	}
	if len(local.Attribs) != 1 || local.Attribs[0] != "const" {
		t.Errorf("attribs = %v, want [const]", local.Attribs)
	}
}

func TestParsesLuaJITIntegerLiterals(t *testing.T) {
	chunk := parseClean(t, `local a, b, c = 1LL, 2ULL, 0x1p-4`)
	local := chunk.Block[0].(*LocalStmt)
	var texts []string
	for _, expr := range local.Exprs {
		texts = append(texts, expr.(*NumberExpr).Text)
	}
	if got := strings.Join(texts, ","); got != "1LL,2ULL,0x1p-4" {
		t.Errorf("numbers = %q, want 1LL,2ULL,0x1p-4", got)
	}
}

func TestLongStringsAndLongComments(t *testing.T) {
	chunk := parseClean(t, `
--[==[ a long comment
   spanning lines, with ]] inside it
]==]
local sql = [[
SELECT 1
]]
return sql
`)
	local := chunk.Block[0].(*LocalStmt)
	lit := local.Exprs[0].(*StringExpr)
	if !lit.Long {
		t.Error("expected the literal to be marked as a long string")
	}
	if lit.Value != "SELECT 1\n" {
		t.Errorf("value = %q, want %q", lit.Value, "SELECT 1\n")
	}
}

func TestOperatorPrecedenceAndAssociativity(t *testing.T) {
	// `..` and `^` are right-associative; `^` binds tighter than unary minus.
	chunk := parseClean(t, `local x = 1 .. 2 .. 3
local y = -2 ^ 2
local z = 1 + 2 * 3`)

	concat := chunk.Block[0].(*LocalStmt).Exprs[0].(*BinOpExpr)
	if _, rightNested := concat.Right.(*BinOpExpr); !rightNested {
		t.Error("expected .. to nest to the right")
	}

	power := chunk.Block[1].(*LocalStmt).Exprs[0].(*UnOpExpr)
	if power.Op != "-" {
		t.Fatalf("outermost op = %q, want -", power.Op)
	}
	if inner, ok := power.Operand.(*BinOpExpr); !ok || inner.Op != "^" {
		t.Error("expected -2^2 to read as -(2^2)")
	}

	sum := chunk.Block[2].(*LocalStmt).Exprs[0].(*BinOpExpr)
	if sum.Op != "+" {
		t.Fatalf("outermost op = %q, want +", sum.Op)
	}
	if right, ok := sum.Right.(*BinOpExpr); !ok || right.Op != "*" {
		t.Error("expected 1+2*3 to read as 1+(2*3)")
	}
}

// A file being edited is the normal case for an analyzer, not the exception.
// What matters is that the functions around the broken statement survive.
func TestRecoversFromABrokenStatement(t *testing.T) {
	chunk := Parse(`
local M = {}

function M.first()
  return 1
end

local = = =

function M.second()
  return 2
end

return M
`)
	if len(chunk.Diagnostics) == 0 {
		t.Fatal("expected a diagnostic for the broken statement")
	}
	var names []string
	for _, stmt := range chunk.Block {
		if fn, ok := stmt.(*FuncStmt); ok {
			names = append(names, strings.Join(append(fn.Path, fn.Method), "."))
		}
	}
	if got := strings.Join(names, ","); !strings.Contains(got, "M.first") || !strings.Contains(got, "M.second") {
		t.Errorf("functions found = %q, want both M.first and M.second", got)
	}
}

func TestUnterminatedStringDoesNotSwallowTheFile(t *testing.T) {
	chunk := Parse("local a = \"oops\nlocal function after() return 1 end\n")
	if len(chunk.Diagnostics) == 0 {
		t.Fatal("expected a diagnostic for the unterminated string")
	}
	found := false
	for _, stmt := range chunk.Block {
		if fn, ok := stmt.(*LocalFuncStmt); ok && fn.Name == "after" {
			found = true
		}
	}
	if !found {
		t.Error("expected the function after the bad string to still parse")
	}
}

func TestMethodCallKeepsItsReceiver(t *testing.T) {
	chunk := parseClean(t, `red:hmset("key", "field", value)`)
	call := chunk.Block[0].(*CallStmt).Call.(*MethodCallExpr)
	if call.Method != "hmset" {
		t.Errorf("method = %q, want hmset", call.Method)
	}
	if recv, ok := call.Recv.(*NameExpr); !ok || recv.Name != "red" {
		t.Errorf("receiver = %#v, want name red", call.Recv)
	}
	if len(call.Args) != 3 {
		t.Errorf("args = %d, want 3", len(call.Args))
	}
}

func TestAdminApiRouteTableShape(t *testing.T) {
	// Kong's api.lua: a returned table keyed by route path, each holding one
	// function per HTTP method.
	chunk := parseClean(t, `
return {
  ["/my-plugin/status"] = {
    GET = function(self) return { status = "ok" } end,
    POST = function(self) end,
  },
}
`)
	ret := chunk.Block[0].(*ReturnStmt)
	table := ret.Exprs[0].(*TableExpr)
	if len(table.Fields) != 1 {
		t.Fatalf("fields = %d, want 1", len(table.Fields))
	}
	key := table.Fields[0].Key.(*StringExpr)
	if key.Value != "/my-plugin/status" {
		t.Errorf("route = %q", key.Value)
	}
	methods := table.Fields[0].Value.(*TableExpr)
	if len(methods.Fields) != 2 || methods.Fields[0].Name != "GET" || methods.Fields[1].Name != "POST" {
		t.Errorf("methods = %#v", methods.Fields)
	}
}

func TestLinesAreRecorded(t *testing.T) {
	chunk := parseClean(t, "local a = 1\n\nfunction f()\n  return a\nend\n")
	fn := chunk.Block[1].(*FuncStmt)
	if fn.Line != 3 {
		t.Errorf("function starts on line %d, want 3", fn.Line)
	}
	if fn.EndLine != 5 {
		t.Errorf("function ends on line %d, want 5", fn.EndLine)
	}
	if chunk.Lines != 6 {
		t.Errorf("chunk lines = %d, want 6", chunk.Lines)
	}
}

func TestShebangIsSkipped(t *testing.T) {
	chunk := parseClean(t, "#!/usr/bin/env lua\nreturn 1\n")
	if len(chunk.Block) != 1 {
		t.Fatalf("statements = %d, want 1", len(chunk.Block))
	}
}

// Deeply nested input must be refused, not crash the process it runs in.
func TestDeepNestingIsBoundedRatherThanFatal(t *testing.T) {
	src := "local x = " + strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500)
	chunk := Parse(src)
	if len(chunk.Diagnostics) == 0 {
		t.Error("expected a diagnostic rather than an unbounded parse")
	}
}

// The corners real Lua actually has. Each of these is something that either
// broke a parser somewhere or reads like it should: the call forms with no
// parentheses, the two places Lua's scoping surprises people, the operators
// added in 5.3 and 5.4, and the literal forms LuaJIT added to 5.1.
//
// A table rather than a test apiece because what matters is the count: a
// parser is judged by how few of these it gets wrong, and a list is how you
// see that at a glance.
func TestParsesTheCornersOfRealLua(t *testing.T) {
	cases := map[string]string{
		"method call with a string argument":                         `require"x" local a = obj:method"lit"`,
		"call with a table argument":                                 `describe{ name = "x" }`,
		"chained call results":                                       `local v = f():g():h().field`,
		"long bracket holding a short one":                           "local s = [==[ has ]] inside ]==]",
		"long comment inside an expression":                          `local x = y --[[mid]] + z`,
		"label and goto":                                             `::top:: goto top`,
		"a local declared in a repeat body is in scope in its until": `repeat local x = 1 until x == 1`,
		"stacked unary operators":                                    `local a = - -5 local b = not not x local c = #{1,2}`,
		"power binds tighter than unary minus":                       `local a = 2^-3 local b = -2^2`,
		"every table field form at once":                             `local t = {[1]=1, 2, 3; x=4, ["y"]=5,}`,
		"numeric literal corners":                                    `local a,b,c,d,e = 0xA.8p1, 3., .5, 1e+10, 0XfF`,
		"a deeply qualified method declaration":                      `function t.a.b.c:d(...) return ... end`,
		"varargs in a table and in a call":                           `local function f(...) return {...}, select("#", ...) end`,
		"semicolons where nothing needs them":                        `;;local a = 1;;; return a;`,
		"a call on a parenthesised expression":                       `local x = (f or g)()`,
		"assignment through an index chain":                          `a.b["c"].d = 1`,
		"a method call split across lines":                           "obj\n  :method()\n  ",
		"the 5.3 and 5.4 operators":                                  `local a = (1 & 2) | (3 ~ 4) // 5 >> 6 << 7`,
		"both 5.4 attributes":                                        `local a <const> = 1 local b <close> = nil`,
		"LuaJIT integer literals":                                    `local a, b = 1LL, 0xffULL`,
		"every string escape":                                        "local s = \"a\\tb\\\\c\\\"d\\65\\x41\\u{48}\\z\n   e\"",
		"closures three deep":                                        `local f = function() return function() return function() end end end`,
		"an if with several elseifs":                                 `if a then elseif b then elseif c then else end`,
		"a generic for over three names":                             `for k, v, w in pairs(t) do end`,
		"a numeric for counting down":                                `for i = 10, 1, -1 do end`,
		"a bare return":                                              `local function f() return end`,
		"a call statement that is a whole chain":                     `a.b.c:d().e:f()`,
		"a comment with no newline after it":                         `local a = 1 -- trailing`,
		"CRLF line endings":                                          "local a = 1\r\nlocal b = 2\r\n",
		"the OpenResty module preamble": "local _M = { _VERSION = '0.1' }\n" +
			"function _M.new(self, o) return setmetatable(o or {}, {__index = self}) end\nreturn _M",
	}

	for name, src := range cases {
		chunk := Parse(src)
		if len(chunk.Diagnostics) > 0 {
			t.Errorf("%s: line %d: %s", name, chunk.Diagnostics[0].Line, chunk.Diagnostics[0].Message)
			continue
		}
		if len(chunk.Block) == 0 {
			t.Errorf("%s: parsed clean but produced no statements", name)
		}
	}
}

// A known gap, pinned so nobody has to rediscover it.
//
// `goto` became a keyword in Lua 5.2, so `local goto = 1` is valid 5.1 and is
// rejected here. It stays rejected: LuaJIT — which is what Kong, OpenResty and
// Neovim all run — backported goto as a keyword, so the code this analyzer
// exists to read cannot use the name either. Accepting it would mean deciding
// per file which dialect is in front of us, from a file that does not say.
func TestGotoIsAlwaysAKeyword(t *testing.T) {
	chunk := Parse(`local goto = 1`)
	if len(chunk.Diagnostics) == 0 {
		t.Error("goto is expected to be a keyword; if this now parses, the comment above is stale")
	}
}
