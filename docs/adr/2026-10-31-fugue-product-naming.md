# ADR: Fugue by SynX product and technical names

- **Status:** Proposed for designer review; none of the proposals below is an accepted product decision yet.
- **Date:** 2026-10-31
- **Scope:** Roadmap R0.0 only. This ADR sets names and migration direction; it does not rename code, data, or repository.

## Context

The roadmap names the product **Fugue by SynX** and requires a naming decision before the R0.4 code/data rename. Today the Go module is `devkit`; the desktop stores its SQLite database at `os.UserConfigDir()/DevKit/devkit.db`; MCP uses `mcp.sock` there plus per-session sockets in `agents/`. Wails/build metadata uses `DevKit`, `com.syuro.devkit`, and `com.devkit.app`. Numerous development, integration, and vendor-facing `DEVKIT_*` variables exist.

The accepted Host API design separately describes a future `devkit serve` host, `host.json`, and local host socket, while preserving the per-session MCP sockets. Its desired host/data-directory lock is not present in the current app. An old binary will not honor a lock introduced by a new binary, so migration must first require the old app to be shut down; a new lock alone cannot make cross-version concurrent migration safe.

## Proposed naming

These are concrete proposals for approval, grounded in the current configuration and Host API. They are not accepted until the designer approves this ADR.

| Concern | Proposal | Notes |
| --- | --- | --- |
| Product/display name | **Fugue**; attribution **by SynX** in product copy | Use “Fugue” for the app name and “Fugue by SynX” for formal attribution. Keep `SynX` as company/publisher. |
| Main executable and CLI | `synx-fugue`; subcommand `serve` (`synx-fugue serve`) | Avoids claiming the generic `fugue` command. This also replaces the Host API's provisional `devkit serve` spelling. The separately invoked MCP stdio proxy is `fugue-mcp`. |
| Go module | Keep `module devkit` until a SynX-controlled repository URL is confirmed; then set the module path to that exact canonical URL | No GitHub owner was supplied or verified, so `github.com/synx/fugue` is not proposed. R0.4's module-path rename is blocked on owner/repository confirmation. |
| Protocol package | `fugue.<module>.v1`; source under `proto/fugue/<module>/v1/` | Matches Host API's module/version scheme while updating its current `proto/devkit/...` example. Domain packages must not import generated proto code, per Host API. |
| User data directory | `os.UserConfigDir()/Fugue/` on each OS | Preserve the platform-specific config root returned by Go; only change the product directory component. Database filename remains `devkit.db` during the migration release, then may become `fugue.db` only in a separately verified follow-up. Keeping the filename avoids needless migration churn. |
| New Host API socket | `fugue.sock` in the Host API's private runtime socket directory; identify its full path in `host.json` | The Host API requires a `0700` directory and `0600` socket on Unix, a user-ACL named pipe on Windows, and a short runtime path when the config path exceeds OS limits. The socket path itself must not become a secret or authorization mechanism. |
| Existing MCP sockets | Retain `mcp.sock` and `agents/<session>.sock` within the new `Fugue/` data directory | These are the existing agent/MCP transport, not the future desktop Host API socket. Keep their protocol-specific names and permissions; do not conflate them with `fugue.sock`. |
| Environment namespace | `SYNX_FUGUE_` for new Fugue-owned configuration and diagnostic overrides | For each renamed Fugue-owned variable, accept the old spelling as an alias for exactly the first two stable releases after the rename. If both are set, the `SYNX_FUGUE_` value wins, including an explicitly empty value; emit at most one deprecation warning per variable name, never its value. Remove the alias after that window. Do not rename variables owned by external tools/providers or API examples (for example `DEVKIT_TOKEN` in generated cURL), wire-protocol identifiers, or historical test fixtures. |
| Bundle identifier and single-instance ID | `vn.io.synx.fugue` for both | Replace the current Wails product identifier, macOS bundle ID, Windows identity, and Wails `UniqueID` consistently. Verify platform-store and signing constraints before release. |
| Repository | Desired name `fugue`, under a SynX-controlled owner | Do not assume GitHub organization `fugue` is available. Keep the current canonical URL until SynX ownership and destination are confirmed; then use the selected URL for repository and module path. |
| Product domain | Desired `fugue.synx.io.vn` | Proposed only; ownership, DNS, TLS, and canonical web destination remain unverified. Do not encode it in clients before verification. |
| Documentation route | Keep the existing `/dev-kit/` project collection and use `/dev-kit/fugue/` as the product landing route within it | This renames the project landing page without changing the established collection prefix used by existing links. On publish, add permanent redirects from the old project landing URL and every renamed page slug; retain historical DevKit pages and references as searchable history. Confirm route ownership with the documentation owner before publishing. |

## Proposed data migration

The upgrade should run as an explicit, recoverable first-start migration from `<UserConfigDir>/DevKit/` to `<UserConfigDir>/Fugue/`. It must not use a directory rename as proof that persisted absolute paths remain valid.

1. Require the old DevKit process to be fully quit before starting migration. The old binary does not honor a new-version lock; a live `mcp.sock` probe is useful evidence but is not a sufficient race-free lock. Do not claim otherwise.
2. If `Fugue/` already exists, never overwrite or merge it automatically. Stop and ask the user to resolve the two profiles.
3. Before any new-version schema write, create and verify an untouched rollback copy of the complete legacy tree, including a consistent copy of `devkit.db` made while the old app is stopped. Do not mutate the original tree. Stage the migration from a second copy and preserve file modes; do not log database contents or secret-bearing metadata.
4. Inventory every path persisted under the data directory before implementation. Current roots are composed in `main.go` (runtime installs, agent homes, checkpoints, PDF engine, agent documents, consoles, and logs), MCP paths in `internal/mcp/host.go` and `internal/mcp/session_host.go`, and CodeGraph indexes in `internal/codegraph/paths.go`. Recreate disposable state (sockets, caches, logs) under the Fugue root; copy durable app-owned state intact. Inspect SQLite path fields such as `workspace_services.path` (`internal/workspace/sqlrepository.go`) and `agent_sessions.project_path` (`internal/storage/sqlite.go`): rewrite only fields proven to point inside the old app-owned root, and leave user workspace/repository paths and all other external absolute paths unchanged. Do not rewrite transcript text, arbitrary payloads, or opaque values. Never perform a blind text replacement. Any unclassified persisted path blocks migration.
5. Run schema migrations only on the staged copy and verify that it opens and passes integrity checks. If an operation fails or its outcome is ambiguous, fail closed, retain the untouched legacy tree and all staged copies, and require recovery review; do not restore a modified DB over the legacy DB or tell an older binary to open a DB whose schema may have advanced. Only after complete verification should Fugue become the new app's canonical data root. Keep the original rollback copy until the user confirms the vault and account state.
6. Never let the new app open the legacy path as a fallback after migration failure. The app must not silently create a second empty profile while presenting migration as complete.

R0.4 must add tests for interrupted migration, pre-existing destination, untouched rollback copy, DB integrity and schema failure/ambiguous outcome, vault/history/account preservation, path inventory and rewriting, unchanged user workspace paths, and prevention of concurrent use of the same data directory by new binaries. Since legacy binaries cannot honor a new lock, the implementation must make the offline/shutdown precondition explicit and fail closed if it can establish that the old process is still active; it must not promise to eliminate a simultaneous-launch race without a mechanism the old version also honors. No old data should be deleted as part of the first rename release.

## Source basis

- Roadmap and naming scope: `/Users/syuro/Workspace/PERSONAL/dev-kit/base-doc/content/projects/dev-kit/core-harness/implementation-roadmap.mdx` (R0.0 and R0.4).
- Host process, socket, protocol, and transition design: `/Users/syuro/Workspace/PERSONAL/dev-kit/base-doc/content/projects/dev-kit/core-harness/host-api.mdx` (§§2–4, 7–9).
- Current module path: `go.mod` (`module devkit`); MCP proxy: `cmd/devkit-mcp/main.go`.
- Current data directory and DB open/migrations: `main.go` (`os.UserConfigDir`, `DevKit`, `devkit.db`).
- Current shared and session MCP socket paths and mode: `internal/mcp/host.go`, `internal/mcp/session_host.go`.
- Current Wails display name and `UniqueID`: `main.go`; generated/build identity: `build/config.yml`, `build/darwin/Info.plist`, `build/darwin/Info.dev.plist`, `build/windows/info.json`, and `build/windows/wails.exe.manifest`.
- Current repository remote: `github.com/syurodev/dev-kit` (git remote `origin`).
- Architecture constraint: `docs/agent-reference/architecture-rules.md` (transport adapters remain above application/domain layers; generated proto types stay out of domain packages).

## Decisions requested

Designer approval is needed for the proposed command name, module path, package namespace, environment prefix policy, bundle ID, target repository owner/name, product domain, documentation route, and migration behavior. The repository owner and domain must be verified by their respective owners before they are used externally. Once approved, record accepted choices here and update the Host API's provisional `devkit` names in a separate design-doc change; R0.0 itself changes no existing code or user data.
