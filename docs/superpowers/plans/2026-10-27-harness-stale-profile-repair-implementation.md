# F-C: confirmed UI repair of stale harness profile references

> Publication update, 2026-10-28: the F-C implementation in #377 has merged. This plan is retained as its historical implementation contract, not an instruction to implement it again. Check the activation ledger for remaining independent-review evidence.

Date: 2026-10-27. Status: implementation plan; not implemented. Production remains inert.

## Baseline and decision

F-C in PR #376 (`35a837eef1b5bfa0e4de95fa03f42f548d73969b`, open when prepared) demonstrates that a stored W containing an unknown group, tool-budget target or group-budget target prevents new sessions and cannot be repaired through existing controls. Base main after #375: `782a63c5b4672bff8df9a48b9a60bf8ad1b0d711`. Fetch and recheck before work; read the report from #376's branch if unmerged. The report's `.go.txt` probe logs the failure; turn it into assertions in the regression suite.

Chosen v1: a workspace-level, UI-only repair that removes **only stale registry references**, previews all removals, and always asks through the existing shared approval flow. No automatic migration, silent ignore, full reset, database edit or MCP repair tool. Missing modules do not mean consent to remove their settings. Offer Cancel and explain that restoring the module/build is an alternative.

Removing a stale off entry loses its remembered denial: if that name returns later, inherited/default availability can apply. Removing stale budget entries also loses their limits. The review must state both consequences. This is an explicit potentially widening recovery, never a narrowing edit eligible for AutoNarrowing. The normal resolver remains strict.

This authorizes implementation and a PR, not production activation, real workspace repair, account use, paid requests, AP3/AP4 or executable changes. Read AGENTS.md and the directly relevant harness, customization/recovery, MCP and core-development guides; read module-shell integration before changing navigation. Reuse the existing workspace harness dialog and shared hub.

## 1. Bounded diagnosis independent of session startup

Add a UI-only workspace diagnosis/repair-preparation surface through the existing Controls → HarnessPort → AppService/bridge path. Names may follow repository conventions. It must work when there is no running session and normal WorkspaceProfile cannot resolve W.

- Enforce UI identity and workspace existence in Go before reading or returning data. Agent, terminal and unknown callers are refused even if they can call the method directly.
- Read the actual stored profile with its stamp, and the actual registry from the composed graph. Distinguish missing profile, unreadable/corrupt data and a well-formed profile containing stale references.
- Find unknown keys only in W.Groups, W.ToolBudgets and W.GroupBudgets. Return typed categories, bounded printable names and existing boolean/budget values needed for the exact review. Treat names as untrusted text, never markup/instructions. No servers, commands, secrets, skills or raw profile JSON in diagnosis/audit/errors.
- Cover all stale entries. If the bounded preview cannot represent the full repair, paginate safely or refuse preparation with a useful fixed message; never silently approve removals hidden by truncation.
- No stored profile or no stale references is a no-change state. Arbitrary corruption, unsupported schema, invalid known settings/budgets, unavailable registry and store failures remain fail-closed with accurate guidance; v1 cannot repair them.

Prefer a small pure helper for stale-reference inspection and copy construction. It must not change normal Resolve or make unknown names acceptable at launch/proposal time.

## 2. Preparation, approval and atomic application

Extend the existing proposal service with a narrowly typed repair intent and reuse its prepared-record ownership, expiry, Cancel, single-use Apply, hub confirmation, writer boundary and store CAS. Do not create another approval store/runner or a direct write in AppService/UI. Do not expose the internal repair intent in agent MCP schemas or let ordinary patches opt into permissive evaluation.

Preparation derives the complete removal set server-side from stored W and registry. The caller supplies only workspace identity; Apply supplies the opaque prepared ID. Do not accept arbitrary replacement profiles, executable edits, target sessions, registry facts or approved flags.

Create an explicit evaluation branch for this repair: the invalid original W cannot be fed through normal before-Resolve. Bind the exact original profile/stamp, canonical removal set, post-repair digest, registry identity and relevant V/policy revisions to the prepared review. Treat the missing before-effective state as unavailable, not as empty/default. Set the approval floor to widening (or a dedicated always-confirm recovery tier with equivalent guarantees). If a valid projected copy is used to validate/display the result, it must never stand in for proof that the original profile was valid or narrowing.

After removing exactly the diagnosed keys from a copy, validate it through the existing schema/resolver path for relevant vendors, preserving the incomplete-facts contract and executable restrictions. If any other invalid state remains, refuse the entire operation; no partial write. Preserve all known groups/budget fields, executable fields and unrelated W data byte-equivalently where the existing canonical store permits.

At Apply, re-read/revalidate under existing serialization and CAS. Any W change, registry change, policy toggle or other approval-relevant basis change makes the prepared repair stale. Do not hold locks while the person decides. Missing prompter, headless or server mode refuses; allow-always and AutoNarrowing never skip this confirmation. Replay, cancel, expired IDs and cross-caller use change nothing.

Write only repaired W in one normal store transaction, advancing its revision once. Preserve S, applied references, transcripts, account settings, usage history and active endpoints. No live enable/revoke/publication is part of this workspace-only repair. Report saved success, Live=false, and accurate next-open/reload effects; a successful subsequent launch still passes all existing executable adoption and approval checks. Repair approval is not executable approval.

If the store reports an ambiguous commit, report uncertainty rather than retrying the same mutation or claiming success. Reuse a truthful outcome/read-back contract; an explicit fresh read can establish the current W without touching active sessions. Do not offer session Reconcile as though it repaired an uncertain workspace write.

## 3. Recovery UI and bindings

In the existing workspace harness dialog, render a specific stale-profile state when normal loading fails for this reason. Do not turn every backend error into a repair opportunity. Keep Cancel/close available before Apply; follow existing in-flight rules.

Show the diagnosed categories and exact removals, including stale off/on values and budget values. Explain that current sessions retain their launch basis, repaired settings apply at the next allowed open/reload, and removed settings will not return automatically if a module returns. Let the person choose Cancel to restore the missing module/build instead.

A Repair action prepares the server-derived review and then uses existing Apply/shared-hub confirmation. Reads/preparation never save. Show conflict, failure and uncertain outcomes without optimistic success; retain evidence and allow a fresh read/reprepare. Fence async responses by workspace/epoch; navigation, closing/reopening or stale answers cannot apply/display another workspace's review. Refresh diagnosis and normal controls after a confirmed saved outcome; no automatic session reload.

Keep shell/sidebar/tab structure unchanged. Do not add a Settings link unless an actual reachable repair entry exists there; correct the existing generic "Repair or reset it in Settings" text for unsupported corruption cases. Regenerate committed bindings with pinned Wails after any AppService method change, then build frontend.

## 4. Tests with actual owner services

Use existing composed rigs, temporary databases/repositories and scripted vendor transport/answers. No real profiles/accounts or database manual edits as the user-facing repair. Meaningful assertions:

1. Save optional pdf off, restart without pdf: session launch fails; repair diagnoses and shows the off entry; Cancel/denial leaves W/revision unchanged; approval repairs W and a new session starts with only actually registered tools.
2. Unknown group on/off, tool-budget and group-budget keys, including combined cases: exact removed entries, no lost known fields, revision increments once. Restore the module after approved removal and demonstrate inherited behavior with the warning contract.
3. A healthy profile/no profile is no-change. Corruption, bad schema, invalid known budget and unavailable registry refuse without partial repair.
4. Agent/terminal/unknown identities, absent workspace, stolen prepared ID, replay, expiry and cancellation fail in Go. No new MCP tool/schema field or capability grant.
5. Concurrent W edit, registry identity change and policy off/on while approval is pending stale the repair; approved old removals never erase new settings. Use deterministic barriers, not sleep-based races.
6. Headless/nil prompter refuse; AutoNarrowing on and allow-always still require exact repair confirmation.
7. Store failure/ambiguous commit produces truthful state; no automatic duplicate retry. S, running admission snapshots, applied references/executable guards and usage rows remain unchanged.
8. UI stale-response/navigation/cancel, exact review, conflict/read failure and uncertain outcome behavior. A stale profile has a reachable repair entry even though normal profile read fails. Generic load errors do not offer unsafe repair.

Add focused tests at the pure helper/proposal boundary and composed service/UI levels where they prove distinct behavior. Avoid a parallel test-only authorization implementation.

## 5. Validation and delivery

Run focused tests first; then the acceptance checks using repository toolchain versions:

```sh
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

Run current CI lint. Publish any new tool-visible error code in bridge.keptCodes or document deliberate redaction. Keep architecture boundaries and production-inert tripwire intact. Native UI behavior remains AP3, not satisfied by controller/SSR tests.

Update the F-C report and canonical activation record with exact behavior, tests, limits and fix SHA. Do not rewrite the historical failing evidence; mark it fixed pending independent review. AP1 remains Partial until this fix and all remaining rows have independent review. O-2, AP3/AP4 and the residual directory-swap limitation from #375 are outside this fix and remain explicit.

Use a separate worktree from latest main. Missing old board state is not permission to fabricate task/session IDs or bypass verifiers; use legitimate tasks where needed. Open PR using terminal gh, attach it to the Codex task, report validation and stop for review. Do not auto-merge.
