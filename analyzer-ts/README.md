# analyzer-ts — the TypeScript analyzer's toolchain

The measurements in this file are why the analyzer is built the way it is, and they were
taken before any of it existed. `cmd/spike` is the throwaway that took them, kept because
re-deriving the protocol is the expensive part. For the analyzer itself, see
`internal/lsp` (the client) and `internal/codegraph/analyzer_ts*.go`.

## What it is

A Go module of its own, not part of DevKit's. It pins
[`microsoft/typescript-go`](https://github.com/microsoft/typescript-go) and
builds `tsgo`, which is the TypeScript compiler rewritten in Go.

A separate module on purpose: `tsgo` is a program DevKit *spawns*, never a
library it imports — everything inside typescript-go lives under `internal/`,
so it cannot be imported even if we wanted to. Putting it in the root `go.mod`
would drag its whole dependency tree into the app's own build for a binary that
is only ever executed.

```bash
cd analyzer-ts
go build -ldflags="-s -w" -o bin/tsgo github.com/microsoft/typescript-go/cmd/tsgo   # 28 MB
go build -o bin/spike ./cmd/spike
./bin/spike "$PWD/bin/tsgo" /path/to/ts/project        # the compiler API
./bin/spike "$PWD/bin/tsgo" /path/to/ts/project lsp    # the LSP
```

## Why not in-process, like Lua

Lua's analyzer is a function call in DevKit's own process, and that is the best
thing about it: nothing to install, ready at startup, works in every package.
TypeScript cannot be that. `typescript-go` keeps its parser, binder and checker
under `internal/`, so there is no importable library — only the `tsgo` binary.

The next best thing is what this is: a subprocess with **no runtime
requirement**, because `tsgo` is a self-contained Go binary that
cross-compiles alongside DevKit itself. No JDK, no Node, no setup panel. It is
28 MB stripped, with `lib.d.ts` embedded — against the 57 MB a bundled JRE was
measured at in syurodev/dev-kit#28, and unlike a JRE it is one static file the
existing packaging table can carry.

## The two ways in, measured

`tsgo` speaks two protocols, both JSON-RPC over stdio with LSP-style
`Content-Length` framing. Measured on a generated NestJS project — 120 resource
modules, **722 source files**, controller/service/repository/entity/dto/module
apiece, with `@nestjs/common` and `typeorm` stubs so imports resolve:

### `tsgo --api --async` — the compiler API

```
initialize                6ms
updateSnapshot          112ms   1 project, 722 files from tsconfig
getSourceFileNames        1ms   780 total (722 the project's own)
getSymbolsOfSourceFiles  16ms   722 module symbols, ONE batched call
getExportsOfSymbol       64ms   722 calls, 89µs per round trip
TOTAL                   207ms
```

Fast, and it is the real compiler: `resolveName`, `getTypeOfSymbol`,
`getSignaturesOfType`, `parseJsonConfigFileContent`. **But it cannot answer
"what does this function call."** A compiler API has no such question, and the
only route to one is `getSourceFile`, which returns a base64 binary AST in an
encoding internal to typescript-go — the most likely thing in the whole
repository to change under us.

### `tsgo --lsp` — the language server

```
initialize                 8ms
documentSymbol × 722    1.47s   6725 symbols, 1081 callable
callHierarchy × 1081     427ms  1320 outgoing edges, 395µs per callable
TOTAL                    1.90s
warm re-index of 1 file  4.95ms
```

`textDocument/documentSymbol` gives the nodes, `callHierarchy/outgoingCalls`
gives the edges — resolved by the checker, across files, constructors included:

```
res0.controller.ts:12 → list      (res0.service.ts)
res0.service.ts:12    → findAll   (res0.repository.ts)
res0.service.ts:24    → Res0Entity (res0.entity.ts)
```

That is CodeGraph's entire data model, in a **standard, stable protocol**, with
no binary AST to decode. 1.9 s for 722 files is the same order as the Lua
analyzer (1.3 s for 1560 files).

## What this decided

All three of these are now how the analyzer works.

1. **Build on the LSP, not the compiler API.** Standard protocol beats an
   internal one, and it is the only one of the two that answers the question
   the graph is made of. Keep the API client for the things the LSP cannot say
   — `parseJsonConfigFileContent` for module layout, `getTypeOfSymbol` for
   signatures — if they turn out to be needed.
2. **The TypeScript analyzer has to be a long-lived process.** Java's and Lua's
   are one-shot: spawn, analyze, exit. A cold TS index is ~2 s but a warm
   re-index of one changed file is **5 ms**, and the file watcher re-indexes on
   every save. Throwing the process away between saves would turn a 5 ms job
   into a 2 s one. `Analyzer` gained a lifecycle (`analyzerCloser`), which
   neither existing analyzer needed. Measured on the finished analyzer: 1.18 s
   cold for 722 files, 268 ms to re-index — and the difference is entirely
   `sync` telling the server only about files whose content hash changed.
3. **`Toolchain` needs a third mode.** Today it has `Native` (in process, Lua)
   and artifact-plus-runtime (Java: a jar and a JDK, where `Run[0]` names a
   `Requirement`). `tsgo` is a third shape: the artifact is itself the
   executable, so the toolchain is `Ready` as soon as the artifact exists and
   there is no requirement to probe.

## Caveats on the numbers

The fixture's `node_modules` is two hand-written stubs, not a real install. A
real NestJS tree pulls in hundreds of megabytes of `.d.ts`, and parsing those
is work this measurement does not include — the cold number will be worse, the
warm number should not be. Re-measure on a real repository before committing to
the design.
