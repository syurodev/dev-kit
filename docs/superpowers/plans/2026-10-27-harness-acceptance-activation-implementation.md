# Harness acceptance and production activation after H5b-2

> Publication update, 2026-10-28: #375–#378 have merged; current main is `a4b64bcc`. Historical baseline/status rows below are preserved. The next focused R1 delivery is `2026-10-27-harness-rewind-finish-forward-implementation.md`; AP1 remains Partial and AP3/AP4 are not accepted. Recheck repository state before execution.

Date: 2026-10-27. Status: **plan prepared; acceptance not performed; production remains inert**.
This is the next acceptance workstream, not an assertion that an official H5c implementation already exists.

## Starting point and sources

Verified against fetched main `e39d9060d11bf7be0643098fe2739358b1c9b577`:

| Slice | Repository evidence | Meaning |
| --- | --- | --- |
| H4a/H4b | #357/#358, guidance fix #359 | Scoped serving, budgets and guidance integrated |
| H4c | #360 and lifecycle/publication fix #363 | Live S coordination integrated; independent acceptance still missing |
| H4d | #366 and endpoint composition repair #367 | Assembly integrated; production construction blocked |
| H5a | #370 | Public group controls, editor and recovery controls integrated |
| H5b-1 | #371 | Plain-text headTail cutter and truthful measurements integrated |
| H5b-2 | #372 | Partial budget edits, budget proposals and UI-owned narrowing opt-in integrated |
| Production | main.go does not pass Harness or call ComposeHarness; TestProductionDoesNotWireTheHarnessYet remains | Tools/UI are not activated |

#372 is merged and its reported GitHub checks were green when this plan was prepared. That is not an independent code review or evidence of native/runtime behavior. The old machine's task board is stale and must not override repository evidence.

Read AGENTS.md and the directly relevant guides first. Source of activation gates: [Harness composition and activation record](../../agent-reference/harness-composition-activation.md), especially A6. Also read [subscription harness spec](../specs/2026-10-26-subscription-harness-tuning-design.md), [phase 2 / TEAM roadmap](2026-10-26-harness-phase2-team-snapshot-implementation.md), [MCP guide](../../agent-reference/mcp-tools.md), [accounts guide](../../agent-reference/agent-hub-accounts.md), and the customization/recovery guide where relevant. Use the current code/contracts if an older plan's execution record is stale.

## Scope and delivery

Accept **group controls + result budgets + the existing narrowly scoped auto-narrowing option**, then make one focused production wiring change. Keep AutoNarrowing off by default. Executable proposals, AllowExecutable, full vendor-file facts, accounts/models/modes/sandbox changes, TEAM dispatch, lessons and quota optimization algorithms are outside this workstream. FactsComplete remains false where the real reader is incomplete; do not relabel that as verified.

| Step | Deliverable | Exit / PR boundary |
| --- | --- | --- |
| AP1 | Independent review of the integrated publication/lifecycle/approval path | Written findings and exact reviewed SHA; fix findings in focused PRs, then review their heads |
| AP2 | Missing composed Tasks/Git guard tests and remaining backend regressions | Focused tests PR; real local services and temp repositories, production wiring unchanged |
| AP3 | Real-runtime, real-wire and native approval evidence | Reproducible fixtures + evidence record; use an isolated unmerged activation candidate where native wiring is needed |
| AP4 | Small production activation PR | AP1–AP3 evidence attached, required checks pass, then ordinary human PR review/merge |

AP1/AP2 can proceed while another owner prepares runtime fixtures; do not edit the same files concurrently. This plan does not dispatch other sessions or authorize cross-thread messages. Use a separate worktree. No production feature flag, environment toggle or fallback runner is added to get around the acceptance gates.

## AP1 — independent review, not another self-review

After #375 merged, its scope-limited report left AP1 partial. Continue with the focused [AP1 completion plan](2026-10-27-harness-ap1-completion-implementation.md); AP2's focused delivery is merged, while AP3/AP4 remain unperformed.

A reviewer who did not author H4c must inspect the **current integrated tree**, not only #360's original diff or the author's test report. If the receiving implementer authored that code, record that limitation and obtain a separate reviewer; do not sign off your own work as independent evidence.

Trace and verify:

1. Calls capture one immutable admission snapshot for groups, resolved budget and measurement revisions; no DB/vendor read on the hot path. Revoke applies to subsequent admissions, not a promise to roll back an already-running handler.
2. Approval describes both saved and actual live effects under pinned launch V/W. A newer saved W cannot silently become the running session's basis. W+S CAS, V serialization and policy revision revalidation remain coherent, including off/on policy toggles during a pending approval.
3. Durable success versus live success are distinct. Unknown commit or failed publication never reports Live=true; quarantine/reconcile remains fail-closed; clients do not retry a committed mutation as a fresh proposal.
4. Endpoint-incarnation fences survive replacement, stale sockets/tokens, Close, reload, account reopen and a crash between commit and publish. A late Close cannot remove a new socket, stale S or a newer quarantine.
5. #353/#363/#367 invariants: explicit Close tombstones S but retains the executable reopen guard; resumed old sessions cannot adopt W executables; rewind commits its applied reference and conversation cut together; incomplete live-publication endpoints are refused before accepted runtime publication.
6. Public facade: authenticated self/workspace scope, UI-only setters, finite bounded proposals/results/cursors, exact before/after budget review, single-use/cancel semantics and fixed public errors. No way for agent input to set policy, approval or executable fields.
7. H5b: independent set/reset/omitted budget fields preserve unrelated W; inheritance is per field; mixed tiers take the strictest classification; fixed exemptions and error/structured content remain truthful. Only an agent's pure narrowing budget edit may skip its question when the UI opt-in is on; group edits, window edits, widening, headless and missing prompters still ask/refuse as specified.
8. No new tool/group/pre-approval widens capability grants, environment rules, repo scope or task ownership. Discovery refresh remains qualified until measured per vendor.

For each actionable finding, supply a tight file/line, concrete trigger and deterministic reproduction where feasible. Keep review-only repros outside the source tree or in a Go overlay until a fix owner incorporates them. Recheck the final head after fixes. Record no-findings with the scope and limits actually reviewed; neither merge status nor green CI substitutes for this row.

## AP2 — exercise the real guard owners through the real controls

Extend the composed app fixtures using real HarnessStore, preferences, workspaces, Tasks, Git services, gateway, hub, MCP server and SessionHost. Only vendor/model responses and the person's answers may be scripted. Do not write a parallel authorization implementation inside tests.

Use temporary local working repositories and a local bare remote; pushes must never reach the person's real remotes or credentials. Create real fixture sessions/tasks through their owning services. Production identity comes from host/session context, never a caller-selected workspace/session claim.

| Case | Required assertion |
| --- | --- |
| Group off | tools/list hides it; cached/direct call is refused before the handler |
| Group enabled by public proposal | Existing grant is still required; the host performs the final actual operation |
| Task ownership absent/wrong | Protected task/branch mutation is refused; adding group availability cannot manufacture a claim |
| Branch guard requiring confirmation | Every guarded mutation still follows its guard; a denied confirmation changes no branch/ref/file |
| Push/task binding | Push requires the current permitted branch recorded on the task the caller holds; default/other/unowned branch cannot be pushed |
| Repository outside workspace | Refused before repository operation; an absolute repo path or alternate worktree is not a scope bypass |
| Repository in workspace but read-only for caller | Read permitted as contracted; writes refused; group enable does not widen granted paths |
| prod and unknown environment tags | Ask every call; allow_always ignored; denial final; no subsequent shell/other-tool retry |
| Terminal / agent / window | Profile and policy operations retain their Go-side identity rules; the terminal is not assigned a Hub session's S |
| Positive control | Correct owner, branch, repo grant and approval can push to the local bare remote; refusals are not explained by a broken fixture |

Add assertions for actual repository refs/state, durable task bindings and consent counts, not only error text. Preserve existing writer admission, package ownership and retention behavior. Do not alter guard policy or a board verifier to get a test past a refusal.

Retain focused integration coverage for the post-H5b surface: budget edits remain saved-W-only until reload; later live S enable uses the old pinned budget; all-on/default/empty/optional-module launches stay coherent; an invalid legacy profile fails with recovery guidance instead of falling back unfiltered; data loss/unknown facts are not an empty valid profile. Unexpected failures become explicit blockers or focused fixes, not waived assertions.

## AP3 — real processes and native approvals

The first four A6 rows remain mandatory: independent review, composed guards, real Claude/Codex/opencode processes, and native scope/confirmation dialogs. SSR/gallery/controller tests do not satisfy native approval, and an in-process scripted transport does not satisfy a real runtime.

1. Inventory supported installed runtime versions and the existing real-runtime fixture patterns. Prefer actual vendor processes under their existing jail/launcher with isolated HOME/config/data, a local mocked-model service, synthetic repositories and no real credentials. Confirm the runtime can exercise the intended protocol without contacting a billed provider before sending a prompt. A shell stub is not a real process acceptance result.
2. This planning approval does **not** authorize sign-in, extracting account files, using the person's real subscriptions, changing real workspace profiles or making paid requests. If a required runtime cannot run safely with local fixtures, report the exact missing environment and wait for explicit user instructions for any dependent account/billing action. Other independent acceptance work can continue.
3. For each real runtime cover default/no-W/S compatibility; opening a narrowed profile; hidden-tool direct refusal; a request to enable an available group; the actual gate-owned delegation of harness.profile.propose on the wire; fixed launch pre-approvals and ordinary permission flow for live-enabled tools; budget/cut behavior as observed from the server; reload/restart and one recovery path. Inspect messages/configuration with sanitized captures, not secrets, prompts or full transcripts in logs.
4. Record tools/list_changed separately: receipt/delivery is not proof of vendor refresh. Compare actual tools discovery/call behavior before and after S changes. Unmeasured or unsupported refresh retains DiscoveryReloadRequired and explicit Reload; this row need not fabricate an automatic-refresh success to pass the conservative product contract.
5. Native desktop smoke needs the real scope-selection and confirmation components connected to the shared backend hub. Test no preselected scope, focused Cancel, denial, approved enable, revoke, publication failure/reconcile, exact budget review, default-off auto-narrowing, and stale/late answer behavior after navigating between sessions. Confirm UI policy setters refuse agent/terminal contexts in Go independently of visual hiding.
6. Run native acceptance on a **local activation candidate** in a separate worktree and disposable app data directory. It uses the same production ComposeHarness path intended for AP4, not a second runner or bypass. Keep the candidate unmerged and clearly label it as acceptance-only; do not enable the person's existing installed app or mutate their current vault/workspaces. A local candidate is how native evidence can precede a production merge. The final tripwire replacement and evidence update belong together in AP4; do not suppress an existing failing test to call the candidate green.
7. Verify actual desktop/server restrictions in the candidate: locked startup, prompts unavailable/headless fail closed, server bridge remains loopback-only and server-mode sessions remain refused. No shell fallback for hidden guarded actions. Record what shutdown/restart does to S, pending metadata and usage collection without restoring old answerable approvals.

### Evidence format

Update the activation record with one row per gate/runtime. Do not rewrite old tests as if they were new evidence.

| Field | Required value |
| --- | --- |
| Candidate | Exact SHA/diff; runtime, OS, native/server build and toolchain versions |
| Fixture | Real process vs scripted transport; mock-model vs paid endpoint; isolated data location (no sensitive payloads) |
| Run | Command or concise repeatable native steps, date, result and sanitized artifact reference |
| Safety | Grants/scope/guard and confirmation outcomes; cleanup; network/billing classification |
| Limits | Skipped, unsupported, unmeasured or blocked, plus the concrete reason |
| Review | Reviewer identity and reviewed SHA, distinct from authorship where independence is required |

Current state at plan creation: AP1 not performed by this document; AP2 missing cases; AP3 real-runtime/native evidence absent. These are not passed because the plan was accepted.

## AP4 — publish a focused activation PR after evidence

Prepare the candidate from the latest main plus accepted fixes. Convert it into a ready-for-review production PR only when AP1–AP3 have supporting evidence. Human merge/release remains the ordinary final approval; do not auto-merge.

1. Pass `Harness: container.Harness` into the existing MCP dependencies **before** the registry is frozen so the two public tools are registered. No late AddTool, second registry, global or environment flag.
2. Build the existing session host and assign agentSvc.MCPEndpoints before any listener/session/UI can be admitted. ComposeHarness must use the actual inventory, real store/preferences/hub/workspaces/vendor registry and the same Agent Hub instance. Bind the port once. Start the shared listener only after successful composition; the current main starts ServeUnix earlier and must be reordered.
3. Initialization failure closes staged resources and aborts startup with a safe error. Never serve a partially bound graph or silently set Harness=nil and run unfiltered. Keep typed-nil checks, endpoint publication handles and server-mode behavior.
4. Replace TestProductionDoesNotWireTheHarnessYet with positive startup-order/construction/failure-path assertions. Do not merely delete or invert a string-search test. Exercise the actual construction path through a narrow reusable seam if extraction is necessary; avoid tests that duplicate the wiring algorithm. The same PR updates the evidence/gate decision to the exact candidate tested.
5. Recheck no-profile compatibility and startup with existing stored profiles, absent optional modules, old data, corrupt/unreadable profiles and vault lock. H5 entry points must appear only for a bound graph; loading/error states are not empty profiles or successful revokes. Default-on groups and default head/24 KiB budgets remain unchanged unless the person actually saved a profile.
6. Preserve account #354/#365 behavior and the H5b UI-only monotonic policy revision. Executable proposals remain refused; no AllowExecutable opt-in is introduced. Do not mix the unrelated missing vendor-facts reader or TEAM C1 implementation into activation.
7. Verify shutdown order: endpoint close and admission invalidation, MCP cancellation, bounded metric drain and transcript shutdown; shutdown is not explicit Close and does not erase durable S. Failed/unknown publication state stays recoverable. No orphan sockets/handlers after failed startup.
8. Document rollback as a deliberate code/deployment revert preserving profile/override/policy/usage data and schema. Do not delete records, reset user settings, force-push or add runtime fallback-on-error as a rollback mechanism.

Required final checks use the module's actual Go version (currently go.mod 1.27.1), Bun and Wails v3.0.0:

```sh
# Real frontend assets must exist before embedded composition checks.
cd frontend
bun install --frozen-lockfile
bun test --isolate
bun run build
cd ..
go test -race -timeout 40m ./internal/harness ./internal/harnessconfig ./internal/agenthub ./internal/mcp ./internal/app ./internal/preferences ./internal/interaction ./internal/usagemetrics ./internal/storage ./internal/bridge
go test ./internal/arch/
go test ./internal/... ./cmd/... -count=1
go vet -tags server ./...
go build -tags server
```

Run current CI lint and scoped staticcheck; regenerate bindings only for AppService signature changes using the pinned Wails version. The H5b reports mention a host-specific CRLF Git test failure and earlier reports mention a fakeCaller race: reproduce against the actual base before calling anything environmental. Fix fixture isolation or disclose the exact unresolved base failure; do not weaken assertions, silently skip a test, bypass sandbox/verifier refusal or change global Git settings. Passing CI does not substitute for the AP3 native/runtime rows.

## Continuation on another machine

All decisions and evidence must be committed into the repository/PR; local temp overlays, an agent chat and the original DevKit board are not the source of handoff truth. Fetch main and inspect the current PR/commit state before choosing the next unfinished AP step. If this plan's docs PR has not merged, fetch/read its branch explicitly instead of assuming the file is on main. Do not reset a dirty checkout or overwrite local documents.

Use a separate worktree for implementation. Missing DevKit tasks on the new machine are not a prerequisite blocker for reading, review, planning or local tests. If ownership/branch tracking is needed for a guarded action, use a legitimate local task/board through the supported workflow; never claim nonexistent old task ids, fabricate session ids or bypass a verifier/branch guard. Old OPT ids are context only and are not portable credentials. No task is marked complete by this document.

After production activation is accepted, the next separate roadmap slice is **C1 TEAM preflight/package binding to actual harness snapshots**. Executable proposals require a separate design with full facts/exact executable confirmation; they are not silently bundled here. TEAM dispatcher, result acceptance and dashboard remain later work.

### Copy/paste handoff prompt

```text
Continue DevKit harness acceptance after H5b-2, using the repository as the source of truth.

Read AGENTS.md and docs/superpowers/plans/2026-10-27-harness-acceptance-activation-implementation.md, then docs/agent-reference/harness-composition-activation.md. Fetch and inspect current main; H5b-2 was merged as #372 at e39d9060 when the plan was written. Recheck later changes instead of replaying an old phase.

This machine has DevKit but no old task board. Old OPT ids are reference only; do not fabricate a task/session/claim or bypass guarded actions. Use a legitimate new local task if needed. Work in a separate worktree.

Start with AP1: inspect the current integrated H4c/#363/#367, #353 and H5 controls/policy path. Produce an independent review with concrete reproductions and exact SHA; if you authored that code, disclose that and obtain independent review. Then implement AP2's missing real Tasks/Git composed guard tests in a focused PR. Keep production inert.

AP3 real-runtime/native evidence and AP4 activation are separate gates. No paid model call, account use, sign-in, real workspace setting change, feature flag, executable proposal or production activation is authorized by this prompt. Keep blocked evidence explicit; do independent work that can proceed. Open PRs with terminal gh, report tests and limits, and do not auto-merge. Stop after the AP1 report/AP2 tests PR for review before progressing to activation.
```

## Continuation after #376 / #377

Use the [remaining acceptance and TEAM handoff plan](2026-10-27-harness-remaining-acceptance-team-implementation.md) for R1 (remaining AP1), R2/R3 (AP3), R4 (AP4) and the bounded C1 handoff. It records open review limits rather than treating the F-C fix as full AP1 completion.
