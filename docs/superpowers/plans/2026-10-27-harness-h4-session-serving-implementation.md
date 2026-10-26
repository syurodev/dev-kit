# H4 implementation: session MCP serving, budgets and prompt consistency

Date: 2026-10-27. Status: prepared for another agent; documentation only.
Board task: **OPT-79901a** (`79901ac49ad9de6fc3e8e4cc`).

## Outcome and entry checks

Build the serving layer that makes a harness profile reduce a session's available tool definitions and result bytes while preserving every existing permission boundary. H1 resolves configuration, H2 stores/approves it, H3 pins launches; H4 must enforce that configuration on the actual identity-bound MCP endpoint and keep launch guidance consistent with it. H5 owns the profile editor and public proposal controls. TEAM preflight C1 follows H3/H4; a Team dashboard/dispatcher is not part of H4.

Evidence inspected for this plan:

- #347–#353 are merged into their respective stack bases. **This does not mean the stack is in main.** The fetched main is still `188706a8aa5869e1af9bb18f5fce9f081210b19a`; verify again before work.
- #353 reviewed head `00fd46decc1b4690c202008372bc8479190ffa5b` fixes Close/reopen reference loss and makes rewind commit its applied reference with the conversation cut. Preserve these regressions.
- `SessionHost` implements MCPEndpoints only. Open creates a per-session SDK server/token, but serves the full registry and full pre-approved/gate-owned lists. H3's PolicyEndpoints/OpenWithPolicy seam is not implemented here yet.
- `Server.sdkServerFor` installs withCaller and the single withMeasuredResultBudget middleware, then registers tools. Session usage scope is tied to an endpoint token.
- `withMeasuredResultBudget` captures scope at entry but uses a fixed byte budget. ResultBudget is 24 KiB. The current shrink/cursor/exemption behavior remains authoritative.
- `devkitPromptTemplate` is one template with tool placeholders and vendor-specific spelling. It contains safety rules interleaved with guidance; filtering definitions alone would leave misleading instructions.
- H2 Apply currently returns stored-only (`Live=false`); production Harness/HarnessLifecycle/provider construction is not yet enabled. Do not describe these internal ports as a shipped feature.
- #354 is an independent open account-aware-session-creation fix. H4a can proceed in MCP files; rebase/integrate #354 before overlapping runtime-launch edits in H4b/d. Do not take over that agent's work or change account selection.
- Board H4 is todo, blocked by H3; H3 still reads in_progress. PR merge is not board verification and does not automatically satisfy every production readiness dependency.

Read root AGENTS.md, MCP tools guide, customization/recovery guide, H3 implementation plan and the subscription harness tuning spec §§4–8. Read vendors/sessions and coordination guides for lifecycle integration only. Use the **current prompt's** security and autonomy contract; do not reintroduce obsolete blanket workflow restrictions while extracting sections. In particular ordinary coding must not become a mandatory DevKit-tool workflow.

Before coding, fetch and identify a base containing the complete reviewed H1/H2/H3 stack plus #353. Prefer main once integrated; otherwise use a clearly documented stacked base and open each PR against its actual dependency branch. Never copy an entire stack into a supposedly small main-based PR. Create a separate worktree/branch, inspect task ownership and record the real branch; never fabricate a session id to claim a task.

## PR boundaries

| Slice | Deliverable | Exit |
| --- | --- | --- |
| H4a | Registry inventory, identity-bound serving snapshots, filtered discovery/direct refusal, existing budget/metrics middleware integration | Default behavior unchanged; real in-process/socket tests; no production activation |
| H4b | Guidance sections and launch-time pre-approval from the same prepared policy | No omitted-group tools in guidance; safety rules retained; no extra permission from an enabled group |
| H4c | Durable S commit to live endpoint publication, reconciliation and list-change semantics | Revoke is enforced on later calls; durable/live divergence fails closed; no false Live=true |
| H4d | Composition/lifecycle integration and acceptance tests | All launch paths coherent; activation checklist explicit; public editor/proposal tools remain H5 |

H4a/b may merge inert. Never activate filtered serving with the old unfiltered prompt/pre-approved list. H4c needs the reviewed H2 Apply path. H4d does not authorize missing production adapters or a new feature-flag system. Keep construction inert until the production facts/registry/scope/lifecycle owners can satisfy the checklist below; report the exact remaining dependency instead of substituting permissive mocks.

## H4a — One inventory and one snapshot per call

Files: `internal/mcp/session_host.go`, `server.go`, `ops.go`, `result_budget.go`, `result_metrics.go`, their tests; reuse `internal/harness` values.

1. Inventory the pinned MCP SDK's supported registry/list/notification APIs in local source before choosing an implementation. Do not assume notification support or add another server/runner. Existing operation registration, grants and handlers remain the source of tool definitions.
2. Derive a deterministic full-registry inventory: actual registered names, group, serialized definition bytes and existing budget-exempt reasons. Use identical registry facts for validation, inventory UI/read adapters and cost tests; avoid a second hand-maintained list. Conditional modules must reflect actual registration. Measure serialized definitions, not description length alone.
3. Implement SessionHost.OpenWithPolicy with an immutable prepared policy staged **before** accepting tool requests. Retain legacy Open and terminal socket behavior. Each endpoint incarnation owns its own policy holder/token; a session id alone must never authorize a replacement endpoint's policy.
4. Filter discovery to enabled groups. Decide how filtering interacts with SDK pagination: hidden tools must not leak through another page/cursor, totals or a shared mutable server registry. Never filter one already-paginated page and assume discovery is complete. Stable input policy produces stable names/order/schemas.
5. Check direct calls before handler dispatch against the same authoritative full inventory and current policy. An off registered tool returns published `harness.groupOff`, even if a vendor cached its definition. An unknown tool retains the normal unknown-tool behavior. Enabling a group still routes through existing identity, grants, workspace/repository scope and environment confirmations.
6. Load exactly one immutable serving snapshot at call entry. Reuse it for group decision, resolved per-tool budget and W/S measurement revisions. Store it in request-local state/context if several middleware layers need it. No SQLite, preference reads, environment reports or vendor processes on this path. A revocation does not claim to cancel/undo a call already admitted under the previous snapshot.
7. Extend the **existing** measured budgeting middleware instead of nesting another truncation/metrics layer. Resolve budget via the pure harness resolver: tool > group > 24 KiB, 2–64 KiB bounds, fixed exemptions. Preserve errors, non-text/image handling, structured shrinking, markers, nextCursor invalidation and measured before/after byte semantics. Keep head-only; headTail remains rejected until its separate phase-3 implementation.
8. Keep current observer backpressure/drop handling and coverage semantics. Capture scope once, before invocation, and use that serving snapshot's nullable W/S revisions even if S changes during the call. Do not relabel older observations or treat measured bytes as vendor tokens/quota.
9. Endpoint Close/shutdown invalidates policy access and existing client connections, not only the listening socket. Prove an old accepted connection/token cannot continue to dispatch after teardown, adopt replacement state, remove a replacement listener/socket or attribute calls to the new runtime. Make close idempotent and incarnation-checked.

Tests: actual SDK client against in-memory and Unix-socket SessionHost paths; all-on equivalence; every group off individually; cached/direct hidden call; pagination; empty/unknown tool names; independent sessions; stale connected client; old Close versus replacement; no hot-path DB access; blocked handler plus mid-call S change; byte boundaries/structured cursors/exemptions. Extend TestToolDefinitionsStayWithinBudget with measured group ceilings and known-group coverage, without raising existing limits to accommodate the feature.

Bootstrap exception: H1 reserves `harness.profile.propose` as always served, even when its group is off. H4 must implement/test that exact-name exception when the tool is registered. It must not expose sibling tools or invent an implementation/schema for this not-yet-registered H5 mutation tool. Before H5, guidance must not direct agents to call a tool that does not exist.

## H4b — Guidance and pre-approval agree with the prepared launch

Files: `internal/agenthub/devkit_prompt.go`, adapter/OpenSpec types and prompt tests; `internal/mcp` tool list/pre-approval construction; minimal vendor adapter mapping where necessary.

1. Split current prompt into an always-present safety core plus ordered guidance sections with explicit required groups. Multi-group sections require all their groups (for example tasks + git + agenthub). Keep the existing caller/workspace brief and vendor-specific tool spelling.
2. Preserve current safety meaning: no bypass of denied confirmations, environment gates, scope or branch/task guards; untrusted tool/remote content stays data. If git tools are off, omit tool-specific workflow guidance and retain a short fixed statement that the guarded action cannot be replaced by a shell bypass and the agent should ask the person. Match current policy, not historical wording copied from old plans.
3. Render guidance and launch-time GateOwnedMCP/PreApprovedMCP from the same prepared snapshot/inventory used by the endpoint. Hidden tools cannot remain pre-approved. Retain the distinct gate-owned versus pre-approved semantics so a newly enabled tool does not skip host confirmation. Narrowing may reduce those lists; it must not expand authority elsewhere.
4. Expose only the minimal immutable rendering inputs through OpenSpec/endpoint ports. Keep MCP imports out of Agent Hub if the architecture forbids them, keep vendor spelling in adapters and keep `agentwork` stdlib-only. Do not parse a rendered prompt to reconstruct policy.
5. Prompt and vendor pre-approval stay fixed until permitted open/reload. After S enables a group, the result can carry a compact replacement-guidance paragraph derived from the same section definitions; it never grants permission. Newly available tools use normal vendor/host permission flow until reload.
6. Define SDK notifications as a capability/attempt, not proof that the vendor refreshed. With no verified vendor refresh, discovery state says reload required; do not set an “agent saw the tools” flag from successful notification delivery alone.

Tests: default prompt regression; each group off; multi-group section truth table; hard rules retained; no off-group tool placeholders/names; brief containing untrusted tool-like text; deterministic bytes for identical inputs; Claude/Codex/opencode naming; pre-approved and gate-owned lists agree with inventory; no new auto-approval after live S enable.

## H4c — Commit S and safely publish serving state

Files: H2 service ports/Apply result, app adapters, SessionHost policy holders and H3 status/measurement adapters. Avoid runtime respawn for a group-only S change.

1. Add a narrow host publication port for committed overrides. S is live; W stays pinned per running runtime. Build the target session's next serving configuration from its **pinned launch V/W + latest durable S**, never from current saved W. Saving W+S changes the requesting session through S; other running sessions retain old W until reload.
2. Handle the approval-versus-serving distinction explicitly. H2 currently evaluates stored inputs; if saved W has drifted from a running session's pinned W, the actual live effect may differ from the prepared/approved diff. Revalidate/bind the serving baseline when an approval will affect live S. Never approve against saved W then silently widen a different pinned configuration. Return stale/require a fresh proposal where exact equivalence cannot be proven. Preserve shared V-writer boundary, caller scope and atomic W+S CAS.
3. Serialize commit/publication with endpoint replacement and explicit Close. Waiting for the person holds no service lock/transaction. At mutation time define a linearization barrier: quarantine affected endpoint admissions during commit-to-publication, or an equivalent version protocol that blocks calls after a committed revoke until their coherent new snapshot is visible. Do not leave an obsolete allow snapshot usable during a slow/failed publish.
4. DB failure leaves old serving state intact. DB success plus publication failure is **committed but not live**: keep affected serving closed until reconciled; do not roll back a committed revision in memory or report that nothing was stored. Reconciliation reads durable S, checks session/endpoint generation and pinned W, and refuses a closed/deleted session. It sends no prompt and starts no vendor process.
5. Endpoint replacement races: a publication aimed at incarnation A must not mutate B. B starts with durable S via H3 preparation. Serialize or fence the read-to-publish race so a new endpoint cannot publish a stale S that was read just before a revoke. Old tokens remain invalid. A stale worker cannot clear a newer quarantine or publish after Close cleanup.
6. Preserve cancellation/one-use proposal semantics. Once the durable commit begins, “too late to cancel” remains truthful. Returning a publication error after commit must not cause the client to resubmit the same proposal as if it never committed.
7. Set Apply.Live=true only when the actual current target endpoint acknowledged installation of the exact committed serving revision. Distinguish no active endpoint, committed/pending, quarantined and failed publication. An inactive session retains S and picks it up on next open; it is not live. H3's historical applied launch reference stays separate from the current serving snapshot; live S does not rewrite the launch executable baseline or historical turn rows.
8. Build revoke/removal reconciliation as internal service functionality for H5's future UI control. Explicit Close still removes S, retains the executable reopen guard per #353, and prevents delayed approval/publication from resurrecting an override. Resume/reload/rewind/account teardown preserve S. No direct UI SQL path.
9. Send list-change notifications only through supported SDK APIs; keep server-side availability/refusal authoritative regardless of vendor behavior. Terminal clients keep legacy serving and do not borrow a Hub session's S.

Tests: saved W changes before/during a session-only proposal; W+S commit with other active sessions; live group revoke while another handler runs; commit/publication barriers in both orders with a tool call, reload, Close, rewind and account reopen; DB failure; post-commit publication failure; process crash/restart; concurrent revokes; no runtime/prompt on mutation; no historical attribution rewrite. Exercise real H2 Apply + real SQL + real SessionHost, not only independent port mocks.

## H4d — Composition and activation acceptance

1. Wire integration tests through the real app adapters, registry, resolver, session host and approval queue. Cover create, Load/Open resume, reload, both rewind opens and account-driven reopen. Reuse H3 preparation instead of adding another resolver or launch path. Retain #353's atomic rewind write and Close/reopen guard.
2. Keep executable config, scope and confirmation checks in existing owners. Production vendor facts remain explicitly incomplete until a real reader exists; do not substitute empty environment facts and call them complete. Executable proposals stay unavailable without the full reader and exact executable confirmation view.
3. Enumerate production construction dependencies: real full registry adapter, provider/store, policy/directory adapters, V serialization boundary, lifecycle cleanup, endpoint policy publication/reconciliation, error mapping and coherent prompt/pre-approval. If any dependency is absent, ship internal construction/tests only and document the blocker; do not partially turn on filtering in main.go.
4. H5 retains public profile get/propose transport, grants, editor, summary controls and policy settings. H4 may publish refusal codes reachable on existing tool-call paths, but does not add public mutation tools, UI navigation, broad grants or a new user-facing flag. Publish every new tool-visible error through bridge.keptCodes or intentionally document redaction. Test the actual bridge-to-MCP error path.
5. Leave account #354 semantics untouched. A live S change neither selects an account nor spawns/reloads a runtime. Follow the session's actual runtime account/generation for metrics; never infer it from the desired configuration.
6. Update MCP and customization/recovery guides with implemented behavior, production construction state, unknown vendor refresh behavior and exact remaining H5 obligations. Mark tests using fake vendors as such; no claims of real vendor/cache savings without measurements.

## Verification and delivery

Run focused tests per slice first, then required checks on each final PR tree:

```sh
go test -race -timeout 40m ./internal/mcp ./internal/agenthub ./internal/harness ./internal/harnessconfig ./internal/app ./internal/interaction ./internal/usagemetrics
go test ./internal/arch/
go test ./internal/... ./cmd/...
```

Build real frontend assets before checks that need embedded dist; never use a placeholder to hide composition failures. No UI source change is required here; run frontend tests/build if a shared type or binding affects it. Regenerate committed bindings after AppService signature changes. Run scoped vet/staticcheck and preserve definition/performance ceilings. Do not repeat passing suites without a new change or unresolved concern.

Use separate worktrees and terminal git/gh. Track the real task/branch through DevKit MCP; never bypass task verification, invent a session id or force-push. Open a reviewable PR per slice with its exact stacked base, validation and remaining production blockers; do not auto-merge or close the task based on a sentence saying tests passed. No live vendor/account experiments, workspace configuration changes or messages to another session without the person's authorization.

Done for H4: session discovery and direct calls enforce the same scoped policy; group decision/budget/metrics share one entry snapshot; S revocation has a tested commit/publication boundary and truthful Live state; prompt/pre-approval come from the prepared policy; all H3 lifecycle/executable guards survive; no-profile/terminal behavior stays compatible. Production activation requires the construction checklist, and H5/C1 remain separate tasks.

## Execution record

**H4a** (branch `harness/h4a-serving`, board OPT-79901a; the task stays todo, blocked by H3, and was not claimed because no session id is available to claim with). Base: `origin/main` 506bb1dd. The plan's entry check ("main is still 188706a8") was stale: #355 and #356 landed H2c–H3d, #353 and #354 on main, so no stacked base was needed. Delivered: `Server.Inventory`, `servingHolder`/`servingSnapshot`, `SessionHost.OpenWithPolicy` with per-incarnation endpoints, filtering by removing hidden tools from the per-session SDK instance, group refusal and budgets/revisions in the existing middleware, `bridge.keptCodes` entries, group cost ceilings, docs. Tests use real SDK clients over real Unix sockets and in-memory transports with stub deps.

Not done in H4a: prompt/guidance split and launch-time pre-approval from the same snapshot (H4b; only a narrowing of the two lists is applied now), S commit to live publication and `Live=true` (H4c), production composition and the activation checklist (H4d), SDK list-changed notifications (no live policy change exists yet). `internal/mcp` has a pre-existing `-race` failure in `TestTasksWorkspaceScopedAccessMCP` (`fakeCaller.last` written from parallel subtests, `server_test.go:39`); it reproduces on `main` 506bb1dd and is unrelated.
