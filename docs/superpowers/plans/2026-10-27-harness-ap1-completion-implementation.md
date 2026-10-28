# Complete AP1 independent acceptance review after #375

> Publication update, 2026-10-28: #376, #377 and #378 have merged. This is the historical AP1 work plan; use the remaining-acceptance plan and the rewind finish-forward follow-up for unfinished work. AP1 remains Partial; do not repeat merged deliveries.

Date: 2026-10-27. Status: prepared, not executed. Production stays inert.

## Baseline and boundary

PR #375 merged on 2026-10-27. Fetched main is `782a63c5b4672bff8df9a48b9a60bf8ad1b0d711`; its final reviewed PR head was `f8e99e044bfe7feea1c888f6bcb437a3760596bc`. Re-fetch before execution and record the actual reviewed SHA. #375 completes the focused AP2 Tasks/Git delivery; its AP1 report explicitly remains partial. Green CI and the review of its fixes do not complete the untraced AP1 paths.

Parent plan: `2026-10-27-harness-acceptance-activation-implementation.md` in this directory (docs PR #374, still open when this plan was prepared). Read that branch if absent on main. Current integrated evidence is in `docs/agent-reference/harness-composition-activation.md` on main. Do not overwrite it with an older worktree copy.

Deliver one independent review/evidence PR, with focused fixes only for demonstrated findings. No AP3 runtime/account/native activity, production wiring, feature flag, executable proposals, TEAM dispatch or account redesign. No real provider requests, sign-in or real workspace changes. Stop after opening the PR for owner review. AP3 and AP4 remain separate.

A reviewer must not sign off code they authored. Disclose authorship per reviewed area; obtain a different reviewer for those areas or leave the row explicitly incomplete. A separate AI session is review input, not an ordinary human merge approval.

## Step 1 — establish a truthful review ledger

Read AGENTS.md and the relevant harness composition, customization/recovery and MCP guides. Fetch main and create a separate worktree. Read #375's AP1 table, the parent AP1 items 1–8, and the current production-inert tripwire.

Create `docs/reviews/2026-10-27-harness-ap1-completion.md`. For each parent AP1 item record: exact reviewed SHA, author/reviewer independence, entry points and downstream owners traced, invariant, evidence/test command, finding or no finding, and unreviewed limits. Carry forward #375 evidence with its original SHA; do not present inherited tests as a fresh review. Recheck relevant integration changes since that SHA. Preserve AP3/AP4 as not run.

## Step 2 — pinned-basis approval and publication arithmetic

Start with `internal/harnessconfig/{apply,live,snapshot,patch,service}.go`, especially `liveEffectFor`; follow the actual helpers and owning stores rather than relying on these filenames as an exhaustive list.

Independently derive saved W, pinned launch V/W and session S before/after an edit. Trace Prepare → displayed approval → final revalidation → durable commit → publication. Cover:

- Saved W changes while a running session retains an older basis; an S enable must use that pinned basis, including budgets.
- Concurrent W/S edits and stale digest/revision; launch V serialization; policy toggles off/on while the person decides.
- Budget-only workspace edits cannot silently alter current S/live limits. Omitted/set/reset fields preserve unrelated settings and inheritance.
- Exact approval describes both saved and actual live effects. No stale answer can approve a different mutation.
- Durable commit success, unknown commit, publication failure and reconcile remain distinct; no false Live=true or duplicate mutation retry.

Use deterministic barriers or fault injection through existing seams for suspect interleavings, not sleeps as proof. Add regression coverage only for demonstrated missing behavior; do not duplicate already sufficient tests.

## Step 3 — lifecycle, endpoint fences and recovery

Trace `internal/agenthub/harness_{runtime,scope,status}.go`, owning coordinator/store code, rewind/reopen/Close call sites, `internal/app/harness_{lifecycle,live,port}.go` and `internal/mcp/{session_host,serving,serving_live}.go` as relevant.

Verify parent AP1 items 4–5 and #353/#363/#367 invariants across actual callers:

- Close invalidates S/admission but retains the executable reopen guard; shutdown is not explicit Close.
- Old session resume, reload, account reopen and fork cannot adopt newer W executables without the existing checks.
- Rewind persists conversation cut and applied reference atomically; transaction failure preserves both.
- Replacement endpoint incarnations fence old publication tickets, sockets, cleanup and late Close.
- Crash/unknown commit and failed install keep quarantine fail-closed; reconcile cannot restore obsolete state or affect a replacement endpoint.
- Incomplete endpoint implementations fail before runtime admission; nil and typed-nil construction stays closed.

Read existing regression tests as well as running them. No real account reopen or native app interaction is required here: owner services, temporary databases and scripted vendor transport suffice for AP1 code/probe evidence, not AP3 acceptance.

## Step 4 — public facade, H5b and frontend state review

Trace `internal/app/harness_public*.go`, `internal/mcp/tools_harness.go`, bridge DTO/error mappings, preferences policy and result-budget/cutter/measurement code. Review `frontend/src/bridge/harness.ts`, workspace budget models/controllers/dialogs, session controls and policy settings, following their actual call sites.

Verify:

- Authenticated self/workspace scope; agent/terminal cannot invoke UI setters or select another session. Schemas expose no policy, approval or executable controls.
- DTO limits, exclusive forms, invalid/duplicate edits, reset semantics, exempt tools, filter-bound bounded cursors and published public errors.
- Size/cut inheritance and strictest mixed classification; default-off opt-in skips only the contracted agent budget narrowing. Headless/missing prompter and non-budget/group/window edits retain their gates.
- Determine whether cut-only head ↔ headTail changes match the spec's classification. Record the product question explicitly if intent is ambiguous; do not silently invent a new policy.
- Plain text versus structured/error/exempt results retain their intended cutter and measurement semantics; one call uses one admission snapshot.
- UI shows exact before/after values and scope; stale async responses cannot overwrite newer session/workspace state. Cancel/late answers, failed reads, conflict and publication recovery do not display optimistic live success or lose drafts.

Frontend/controller checks are code-level evidence only. Native focus, dialogs and real runtime tool discovery remain AP3. Accounts/vendor adapters need review only at lifecycle touchpoints affected by these invariants; no unrelated audit is required.

## Step 5 — findings, fixes and validation

For each actionable finding provide severity, precise line/entry point, concrete trigger, user/security consequence and deterministic reproduction where feasible. Keep exploratory probes outside the tree or in Go overlays; include the meaningful regression when a fix is delivered. Report instrumentation transparently. Use the existing service/gateway/guard path, never a bypass or a parallel authorization algorithm.

Fix demonstrated defects in focused commits, then re-review the final head. If independence cannot be satisfied for a fix, leave the corresponding acceptance row pending external review. Do not waive a failure to finish AP1.

Run focused affected tests first; then, for this acceptance delivery, run:

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

Use the repository Go/Bun/Wails versions and current CI lint. Regenerate bindings only for AppService signature changes. Distinguish new failures from reproduced baseline failures; no global Git changes, weakened assertions or silent skips. If a test cannot run, record the exact blocker instead of a pass.

## Exit and handoff

Update the canonical activation record with the new report link and item-level results. AP1 is complete only when all eight parent items have independently traced evidence and no unresolved blocking findings; otherwise state Partial and list the exact remaining work. Do not mark AP3/AP4 passed, turn on production, or replace the inert tripwire.

Open a PR using terminal `gh`; attach it to the current Codex task. Include reviewed SHA, findings/fixes, checks and limits. No auto-merge. Missing old DevKit board tasks does not block reading/review/local tests; use legitimate local tasks if a guarded operation needs one, never fabricated IDs or verifier bypasses.

After owner review/merge and actual AP1 completion, the next deliverable is AP3 isolated real-runtime/native evidence, followed by the separate AP4 activation PR.

## Follow-up from PR #376

The report at `35a837ee` leaves AP1 Partial and identifies F-C (stale stored registry references with no in-app recovery). Implement the focused [F-C repair plan](2026-10-27-harness-stale-profile-repair-implementation.md), then obtain independent review; this does not replace the remaining AP1 rows or AP3/AP4.
