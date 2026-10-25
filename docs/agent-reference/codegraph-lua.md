### CodeGraph: the Lua analyzer
`internal/lua` is a hand-written Lua parser (lexer + recursive descent). It reads 5.1
through 5.4 plus the LuaJIT literals, because one Kong tree holds all of it: the plugin
runs on LuaJIT (5.1, `1LL`), the tooling beside it is plain 5.4 (`//`, bitwise, `<const>`).
It never returns an error — a statement it cannot read becomes a `Diagnostic` and the parse
resynchronises at the next statement boundary, so a file being edited keeps every function
around the broken one. (The Java analyzer swallows an unparseable file whole; that is the
failure this deliberately does not copy.)

`internal/codegraph/analyzer_lua*.go` turns that tree into an `AnalysisResult`:

- `analyzer_lua.go` — file discovery, the require-path index, module nodes, doc comments.
- `analyzer_lua_declare.go` — what each file declares and which of it is reachable. Lua has
  no export keyword, so all six ways of putting a function on a module table are read
  (`function M.f`, `function M:f`, `M.f = function`, `local M = { f = ... }`,
  `return { f = ... }`, a bare global).
- `analyzer_lua_resolve.go` — call resolution. **There are no types, so resolution is by
  provenance**: a value is followable when it traces back to a `require` of a file in this
  project, the table the module returns, or a method's `self`. Everything else is a call
  site with a reason and no target — `library` (stdlib, `ngx`, `kong`, a require with no
  file here) or `unresolved` (a value the analyzer lost). A wrong edge is worse than a
  missing one. A constructor (`new`, `create`, `extend`, …) carries its module through;
  an ordinary method does not, because `db:query()` returns data, not an instance.
- `analyzer_lua_kong.go` — everything about what a Lua project is FOR. Entry points are
  Kong plugin phases (`ACCESS · plugin-name`, since a plugin serves no route of its own),
  Kong Admin API route tables (`["/path"] = { GET = ... }`) and Lapis `app:get` routes.
  Layers follow: endpoints → `controller`, `resty.redis`/`kong.db`/a `dao`-ish name →
  `repository`, and what a controller requires → `service` unless it already said otherwise.

Module ids are require paths (`kong/plugins/x/handler.lua` → `kong.plugins.x.handler`,
`foo/init.lua` → `foo`, a leading `lua/` or `src/` stripped). A require that names nothing
exactly falls back to a **unique** suffix match, which is what makes
`require "kong.plugins.mine.util"` find `util.lua` in a plugin repository checked out at
its own root; an ambiguous suffix resolves to nothing rather than to a guess.

`analyzer_lua_e2e_test.go` is the one that matters when changing any of this: it drives a
Lua index through the flow walker, blast radius, go-to-definition, find-references, the
data-flow trace and the snippet reader — all written against the Java analyzer's id and
label contract, all of which a second analyzer can break without failing a single one of
its own tests.

```bash
go test ./internal/lua/ ./internal/codegraph/ -count=1
```

