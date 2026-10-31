# R0.1 accepted decision: harness package and storage ownership

**Status:** Accepted 2026-10-31 by the user; no production code or architecture tests changed.
**Scope:** G-01 package placement, dependency direction, `exec_*` ownership, and app/transport boundary. Product naming is in the sibling R0.0 ADR.

## Accepted decision

Keep `internal/harness` for existing effective configuration, put native execution components under `internal/harnessruntime`, place the execution repository in `internal/execstore`, and keep schema lifecycle in `internal/storage`. **The process root opens/migrates the DB, constructs the runtime, store, and runtime adapters, then injects the narrow contracts interface into `internal/app`** (accepted by the user on 2026-10-31). If that construction point conflicts with lifecycle ownership, return to design review. Wails/bridge and future Connect/hostapi use the app API; existing MCP continues to receive its current injected service/gateway ports and must not import app.

## Packages and direct imports

The import lists below are the complete allowed in-module imports for new packages. Standard library imports are otherwise allowed. SDKs are permitted only in `modeladapter`.

| Package | Responsibility | In-module imports |
| --- | --- | --- |
| `internal/harness` | Existing effective configuration; do not turn it into the runtime package. | None; preserve current rules. |
| `internal/harnessruntime/kernel` | Pure command/event/state types and `decide`/`evolve`; no clock, randomness, I/O, SQL, or SDK. | None |
| `internal/harnessruntime/contracts` | Shared neutral port interfaces and request/result types for repository, model, context, tools, clock, vault observation, and dispatch. | `kernel` |
| `internal/execstore` | Sole runtime SQL repository/invariant owner for `exec_*`; accepts `*sql.DB`, implements repository/key-protection ports. | `kernel`, `contracts`, `crypto` |
| `internal/harnessruntime/coordinator` | Session admission, queue, fencing, scheduling, orchestration and dispatch sequencing. | `kernel`, `contracts` |
| `internal/harnessruntime/contextengine` | Deterministic context construction behind neutral contracts. | `contracts` |
| `internal/harnessruntime/modeladapter` | Provider-neutral adapter implementations; all provider SDK dependencies stay here. | `contracts` |
| `internal/harnessruntime/tools` | Tool registry and neutral contracts, without domain-specific SQL or authorization. | `contracts` |
| `internal/hostapi/<module>` | Connect/protobuf adapter mapping transport DTOs/principal to app methods. | `app`, generated Go packages under `internal/hostapi/gen/**` |
| `internal/app` | Application API and domain-service composition boundary. Exposes/injects the runtime through a narrow contracts interface. Concrete domain tool adapters use services already owned here. | `contracts` for the runtime interface; existing domain imports remain. No direct `storage` or `crypto` imports. |
| `internal/storage` | Existing SQLite opener and schema/migration registry; creates and migrates `exec_*`, not runtime rows. | Existing rules unchanged. |

`internal/tools` already holds generic utilities such as hash/JWT. The runtime tool package therefore lives at `internal/harnessruntime/tools`. Keep `internal/harness` separate because current architecture tests make it a stdlib-only leaf.

## Dependency and composition contract

```text
Package imports:
  bridge, future hostapi → app
  app → contracts (runtime interface)
  coordinator → kernel, contracts       execstore → kernel, contracts, crypto
  contextengine/modeladapter/tools → contracts       contracts → kernel

Composition (arrows here are injection, not imports):
  main → storage.Open + migrations → *sql.DB
  main → execstore + coordinator + context/model/tool runtime adapters → contracts.NativeRuntime
  app → existing domain services → concrete contracts.ToolExecutor
  main injects NativeRuntime into app.Container
  MCP → existing injected service/gateway ports (unchanged; no app import)
```

The process root opens/migrates the shared database and constructs `execstore`, coordinator, and runtime adapters, then injects a narrow contracts interface into app. App exposes that runtime through its application API and builds concrete domain tool adapters against existing owning services/capability gateways. The existing direct app→storage and app→crypto bans remain unchanged. No caller or adapter gains a second execution path.

`contracts` owns shared method signatures so Go implementations do not need to import coordinator. It imports kernel for lifecycle/repository values; model/context/tool DTOs are provider-neutral. Coordinator imports only kernel/contracts. Runtime adapters do not import one another, coordinator, app, hostapi, MCP, bridge, or storage. App-local domain tool adapters depend on existing services and implement the contracts passed into root composition.

Protected payload handling has two distinct crypto steps from `storage-security.mdx` §4: app supplies a narrow key-wrapping port backed by `vault.Service.SealRecord`/`OpenRecord` for the random session data key and `exec.session_key` binding; execstore seals each payload using `internal/crypto` AEAD under that key with associated data bound to payload ID, session ID, kind, and key ID. Execstore owns the cached session key and synchronously zeroes it through the vault lock observation/guard before lock completes. `SealRecord` does not encrypt each payload.

## Storage ownership

`internal/execstore` is the sole runtime reader/writer and invariant owner for the `exec_*` namespace: host/admission, events, projections, receipts, timers, queue, encrypted payloads, session keys, and purge records. All event/payload/projection/receipt/admission writes use one repository transaction. A commit with unknown outcome stops dispatch until read-back by command identity. Other packages use repository methods; none issues SQL against these tables.

`internal/storage` remains the schema owner. Its migration registry creates/upgrades the tables and append-only triggers in the existing shared `devkit.db` (`storage.Open`: modernc SQLite, one connection, DELETE journal, mode 0600). This is schema DDL, not a second runtime writer. In `internal/arch/tables_test.go`, add the `exec_` ownership rule with the first real migration in R1.3: allow runtime SQL in `internal/execstore` and migration DDL in `internal/storage`. Do not add a speculative owner now: `TestEveryOwnedTableStillExists` requires every registered table/prefix to appear in an actual migration. No dummy tables or relaxed existence check.

## Application API and Connect transition

`internal/app` remains the Go application API. Existing `bridge` stays a client of app. `internal/mcp` remains an independent adapter over its injected services; `internal/arch/arch_test.go` forbids MCP→app and that rule does not change. The new `internal/hostapi/<module>` adapter may import app; R0.2 can add that precise importer exception alongside current root/bridge importers. Generated protobuf imports stay inside `internal/hostapi/**`; put generated Go code under `internal/hostapi/gen/**`. Root constructs hostapi but does not import generated message types.

Add the schema/adapter alongside the existing bridge, then migrate app operations to Connect incrementally. Both adapters call the same app methods and preserve identity, authorization, prompt and public-error behavior. Freeze `AppService` as Host API §9 specifies; new UI operations belong in protobuf/application methods, not new Wails-only methods. Wails remains confined to the desktop shell.

## R0.2 architecture-test contract

1. Add literal package-policy entries for all accepted paths, even while absent: kernel `[]`; contracts `[kernel]`; execstore `[kernel, contracts, crypto]`; coordinator `[kernel, contracts]`; contextengine/modeladapter/tools `[contracts]`; app's new runtime import `[contracts]`; hostapi `[app, generated-under-hostapi]`. The process root imports/constructs concrete runtime/store/adapters; app-local domain adapters import contracts and existing owners only. Keep existing closed rules unchanged except app importers gain `internal/hostapi/**`.
2. Test the validator itself with synthetic in-memory allowed and forbidden import graphs. Assert allowed graphs pass and forbidden edges fail (including MCP→app, runtime→transport, coordinator→SQL/storage/SDK, and generated proto outside hostapi). These are validator test inputs, not production packages, migrations, or policy exceptions; they exercise enforcement before production packages exist.
3. When real packages appear, compare their actual import graph against the same policy. Keep generated packages under the hostapi subtree; do not add cross-module generated-code restrictions absent from the protocol design.
4. Add `exec_*` table-owner enforcement with real migrations in R1.3 as described above. Preserve current Wails, cycle, protected-table, stdlib-leaf, prompt, package-doc and file-size rules. No broad architecture exception.

## Evidence and review checks

The placement follows `docs/agent-reference/architecture-rules.md` and `internal/arch/{arch_test,core_layering_test,tables_test,adapters_test}.go`: app composes domain services, direct imports are gated, MCP cannot import app, and table ownership must match real storage migrations. `main.go` opens the shared DB with `storage.Open`; the user explicitly selected process-root construction for the native core/store, injected into app through contracts. Core-harness `architecture.mdx` §1/§8 leaves package placement open but requires a pure kernel, one coordinator and no alternate runner. Host API §4/§9 assigns the future adapter to `internal/hostapi/<module>` and prohibits proto imports in domain code. Event-storage §2–5 and storage-security §2–6/§8–10 supply the single-writer, atomicity, payload, key and vault-observation contracts; this ADR assigns their package owners.

Review check: if process-root injection conflicts with runtime lifecycle ownership, return to review as the user directed. Confirm the key-wrap port and synchronous cache erasure fit the vault API, and that `execstore` plus migration DDL in `storage` matches “storage kernel and execution store package.” No implementation or test change is part of this ADR.
