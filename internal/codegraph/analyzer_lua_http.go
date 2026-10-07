package codegraph

import (
	"regexp"
	"strings"

	"devkit/internal/lua"
)

// luaHTTPClient is the library whose calls are HTTP requests this analyzer
// reads: OpenResty's resty.http, the client every Kong plugin that talks to
// another service uses.
const luaHTTPClient = "resty.http"

// luaRedisClient is the library whose commands this analyzer reads as Redis
// accesses: OpenResty's resty.redis.
const luaRedisClient = "resty.redis"

// recordOutbound records `httpc:request_uri(url, opts)` and
// `httpc:request({ path = …, method = … })` as the HTTP calls they are.
//
// What makes the receiver an HTTP client is provenance, not a name: a value
// that traces back to `require "resty.http"`, which the resolver already
// follows through `http.new()`. A method called request_uri on anything else is
// not one.
func (r *luaResolver) recordOutbound(call *lua.MethodCallExpr, scope *luaScope) {
	if call.Method != "request_uri" && call.Method != "request" {
		return
	}
	client := r.eval(call.Recv, scope)
	if client.kind != luaLibraryValue || client.lib != luaHTTPClient || len(call.Args) == 0 {
		return
	}
	var url string
	var options lua.Expr
	if call.Method == "request_uri" {
		url = r.foldString(call.Args[0], scope)
		if len(call.Args) > 1 {
			options = call.Args[1]
		}
	} else {
		// request({ path = "/x", method = "GET" }): the host was bound at connect().
		table, ok := call.Args[0].(*lua.TableExpr)
		if !ok {
			return
		}
		path := tableField(table, "path")
		if path == nil {
			return
		}
		url = r.foldString(path, scope)
		options = table
	}
	method := "GET" // resty.http's default
	if table, ok := options.(*lua.TableExpr); ok {
		if field := tableField(table, "method"); field != nil {
			method = luaHTTPMethod(field)
		}
	}
	r.result.OutboundCalls = append(r.result.OutboundCalls, OutboundCall{
		From: r.from, File: r.module.rel, Line: call.Line,
		Client: OutboundRestyHTTP, HTTPMethod: method, URL: url,
	})
}

func tableField(table *lua.TableExpr, name string) lua.Expr {
	for _, f := range table.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return nil
}

// luaHTTPMethod reads the method of a request: "POST", or ngx.HTTP_POST.
func luaHTTPMethod(expr lua.Expr) string {
	switch e := expr.(type) {
	case *lua.StringExpr:
		return strings.ToUpper(e.Value)
	case *lua.IndexExpr:
		if key, ok := e.Key.(*lua.StringExpr); ok && strings.HasPrefix(key.Value, "HTTP_") {
			return strings.TrimPrefix(key.Value, "HTTP_")
		}
	}
	return "ANY"
}

// luaStringPassthrough are the calls that hand their argument back as the
// piece of URL it is: escaping a path segment is still that segment.
var luaStringPassthrough = map[string]bool{"tostring": true, "escape_uri": true, "unescape_uri": true}

// foldString is the string an expression builds, with a "{expr}" hole for each
// part only the runtime knows — a parameter, a config field, a call. Literals
// join, `..` concatenates, a local set once to a literal is seen through, and
// string.format fills its %s in order. The same shape the Java side gives a
// URL, so the join treats both alike.
func (r *luaResolver) foldString(expr lua.Expr, scope *luaScope) string {
	switch e := expr.(type) {
	case *lua.StringExpr:
		return e.Value
	case *lua.NumberExpr:
		return e.Text
	case *lua.BinOpExpr:
		if e.Op == ".." {
			return r.foldString(e.Left, scope) + r.foldString(e.Right, scope)
		}
	case *lua.NameExpr:
		if value, ok := scope.get(e.Name); ok && value.hasText {
			return value.text
		}
		return "{" + e.Name + "}"
	case *lua.IndexExpr:
		return "{" + luaExprText(e) + "}"
	case *lua.CallExpr:
		if name := luaCalleeName(e.Func); name == "string.format" && len(e.Args) > 0 {
			return r.foldFormat(e, scope)
		} else if last := name[strings.LastIndex(name, ".")+1:]; luaStringPassthrough[last] && len(e.Args) >= 1 {
			return r.foldString(e.Args[0], scope)
		}
	}
	return "{" + luaExprText(expr) + "}"
}

// foldFormat folds string.format("/v1/%s/api/%s", org, api) to /v1/{org}/api/{api}.
func (r *luaResolver) foldFormat(call *lua.CallExpr, scope *luaScope) string {
	format, ok := call.Args[0].(*lua.StringExpr)
	if !ok {
		return "{string.format}"
	}
	var out strings.Builder
	arg := 1
	text := format.Value
	for i := 0; i < len(text); i++ {
		if text[i] != '%' || i+1 >= len(text) {
			out.WriteByte(text[i])
			continue
		}
		// Skip flags and width up to the conversion letter.
		j := i + 1
		for j < len(text) && strings.IndexByte("-+ #0123456789.", text[j]) >= 0 {
			j++
		}
		if j >= len(text) {
			out.WriteString(text[i:])
			break
		}
		if text[j] == '%' {
			out.WriteByte('%')
		} else if arg < len(call.Args) {
			out.WriteString(r.foldString(call.Args[arg], scope))
			arg++
		} else {
			out.WriteString("{?}")
		}
		i = j
	}
	return out.String()
}

// literalText is the string an expression is, when it is nothing but literals.
func (r *luaResolver) literalText(expr lua.Expr, scope *luaScope) (string, bool) {
	switch e := expr.(type) {
	case *lua.StringExpr:
		return e.Value, true
	case *lua.BinOpExpr:
		if e.Op == ".." {
			left, ok := r.literalText(e.Left, scope)
			if !ok {
				return "", false
			}
			right, ok := r.literalText(e.Right, scope)
			return left + right, ok
		}
	case *lua.NameExpr:
		if value, ok := scope.get(e.Name); ok && value.hasText {
			return value.text, true
		}
	}
	return "", false
}

// luaCalleeName spells a callee as dotted names: string.format, ngx.escape_uri.
func luaCalleeName(expr lua.Expr) string {
	switch e := expr.(type) {
	case *lua.NameExpr:
		return e.Name
	case *lua.IndexExpr:
		if key, ok := e.Key.(*lua.StringExpr); ok && e.Dot {
			if base := luaCalleeName(e.Obj); base != "" {
				return base + "." + key.Value
			}
		}
	}
	return ""
}

// luaExprText is a short spelling of an expression for a hole: `conf.base_url`
// for a field, the callee for a call.
func luaExprText(expr lua.Expr) string {
	switch e := expr.(type) {
	case *lua.NameExpr:
		return e.Name
	case *lua.IndexExpr:
		if key, ok := e.Key.(*lua.StringExpr); ok && e.Dot {
			return luaExprText(e.Obj) + "." + key.Value
		}
		return luaExprText(e.Obj) + "[]"
	case *lua.CallExpr:
		if name := luaCalleeName(e.Func); name != "" {
			return name + "()"
		}
	case *lua.MethodCallExpr:
		return luaExprText(e.Recv) + ":" + e.Method + "()"
	}
	return "expr"
}

// recordRedis records `red:get(key)`, `red:hset(key, …)` and the rest of
// resty.redis's commands as the Redis access they are. The receiver is Redis by
// provenance — a value that traces back to `require "resty.redis"` through
// `redis:new()` — so a method called get on anything else is not one.
func (r *luaResolver) recordRedis(call *lua.MethodCallExpr, scope *luaScope) {
	read, write := redisReadCommands[call.Method], redisWriteCommands[call.Method]
	if (!read && !write) || len(call.Args) == 0 {
		return
	}
	client := r.eval(call.Recv, scope)
	if client.kind != luaLibraryValue || client.lib != luaRedisClient {
		return
	}
	key := r.foldString(call.Args[0], scope)
	// A key that is only a hole names nothing two services could share.
	if strings.TrimSpace(luaHoles.ReplaceAllString(key, "")) == "" {
		return
	}
	access := "read"
	if write {
		access = "write"
	}
	r.result.RedisAccesses = append(r.result.RedisAccesses, RedisAccess{
		From: r.from, Key: key, Access: access, Command: call.Method, File: r.module.rel, Line: call.Line,
	})
}

// luaHoles matches the {expr} holes foldString leaves.
var luaHoles = regexp.MustCompile(`\{[^{}]*\}`)
