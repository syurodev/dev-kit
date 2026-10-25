### CodeGraph: the TypeScript analyzer
A language server, driven. `internal/lsp` is the client — not an editor client: no
diagnostics, no completion, no document sync beyond opening a file. An indexer opens
everything once and asks structural questions, tens of thousands of them, so calls are
matched by id and safe to make concurrently.

TypeScript cannot be in-process like Lua: `microsoft/typescript-go` keeps its parser,
binder and checker under `internal/`, so there is no library to import — only the `tsgo`
binary, which `analyzer-ts/` (a Go module of its own) pins and builds.

`internal/codegraph/analyzer_ts*.go`:

- `analyzer_ts.go` — the analyzer. Owns the long-lived connection, walks the project,
  orchestrates the LSP, assembles the `AnalysisResult`.
- `analyzer_ts_model.go` — the language-level model: files, declarations, decorators,
  imports, and **call expressions** (receiver, name, arguments as written).
  **It knows nothing about any framework.** Calls are here because a call whose meaning
  is in its ARGUMENTS is a language-level fact: the checker resolves
  `app.get('/users', h)` and `client.send({ cmd: 'x' })` to library methods and is right
  to, but the route and the pattern are in the source either way.
- `analyzer_ts_read.go` / `analyzer_ts_scan.go` — what the server does not say. tsgo
  returns an empty `detail`, so signatures come from the source, and no LSP request
  exposes decorators. Bounded text work: the server has already said where each
  declaration starts and where its name is, so **a decorator is whatever sits between
  those two points** and a parameter list is whatever is inside the parentheses after the
  name. Nothing searches for a declaration.
- `analyzer_ts_framework.go` — **the seam**. A framework is a reading of the model, not a
  fork of it: five methods (`Name`, `Detect`, `Endpoints`, `Layer`, `Edges`). Adding one
  is a new file plus a line in `tsFrameworks`; it may not touch the model or the analyzer.
  If it needs something the model does not carry, the model grows a *language-level* fact,
  never a framework-shaped one.
- `analyzer_ts_nest.go` — NestJS, read almost exactly the way the Java analyzer reads
  Spring, because Nest copied the model. One place reads more than a decorator: Nest marks
  every provider `@Injectable`, so the decorator alone cannot tell a service from a
  repository — what the class injects can.
  Nest's entry points are not only HTTP: `@MessagePattern`/`@EventPattern` (every
  transport — TCP, Redis, NATS, Kafka, RabbitMQ, MQTT), gRPC, `@SubscribeMessage`,
  GraphQL resolvers, and `@Cron`/`@Interval` — the last of which are gated on nothing,
  because a scheduled job lives on a provider, never a controller.
  **`client.send(pattern)` is linked to the `@MessagePattern(pattern)` that serves it**,
  as a `call` edge, so a flow walks across the service boundary the way a reader does.
  The checker cannot produce that edge and is right not to: what the call reaches is
  decided at run time by a broker, and the only thing connecting the two is the pattern,
  written on both sides. Patterns are canonicalised (object keys sorted) so a caller and
  a handler written by different people still meet; an ambiguous pattern resolves to
  nothing rather than to one of two handlers.
- `analyzer_ts_next.go` — Next.js App Router. It exists to prove the seam: an interface
  with one implementation is a guess about what varies. Next shares nothing with Nest — no
  decorators, no classes, no injection; a route is a DIRECTORY and a handler is an exported
  function whose name is the HTTP method.
- `analyzer_ts_router.go` — the Express family (Express, Fastify, Hono, Koa's router,
  Restify). The third way a route is declared: by CALLING a method at run time, so the
  only trace in the source is the call. One reading covers all of them. The path is the
  guard — every one of `get`/`post`/`delete` is a method on something else too, and a
  route is the only one whose first argument starts with `/`. tsgo reports an inline
  handler as a function symbol of its own, so a flow starts at the handler rather than at
  the file; a named handler resolves like any other identifier. It does NOT follow
  `app.use('/api', subrouter)`: mounting moves every route beneath it, and reporting one
  at the wrong path is worse than reporting it at its own.

Two things this analyzer does that the others cannot:

- **There is no `unresolved` call site.** The checker resolved the call or the code does
  not compile. A target outside the project is `library`, same as everywhere else.
- **Module resolution is the compiler's.** `tsconfig` paths, `baseUrl`, package `exports`,
  extension inference, NodeNext's `.js` meaning `.ts`, barrel re-exports — asked by running
  `textDocument/definition` on each import specifier rather than reimplemented and got
  silently wrong.

**The process is long-lived and that is the design.** A cold index of 722 files is ~1.35s,
almost all of it building the program; a re-index is ~320ms, because `sync` tells the
server only about files whose content hash changed. The file watcher re-indexes on every
save, so a one-shot subprocess would pay the cold cost every time.

Tests skip unless the analyzer has been built (`task build:analyzer:ts`, or directly:
`cd analyzer-ts && go build -o bin/tsgo github.com/microsoft/typescript-go/cmd/tsgo`):

```bash
task build:analyzer:ts
go test ./internal/lsp/ ./internal/codegraph/ -count=1
```

