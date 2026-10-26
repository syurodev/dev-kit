# H4b implementation: guidance and launch permissions from the prepared tool policy

Date: 2026-10-27. Status: prepared for another agent; no implementation in this document.
Parent task: **OPT-79901a** (`79901ac49ad9de6fc3e8e4cc`). Parent plan: [H4 session serving](2026-10-27-harness-h4-session-serving-implementation.md).

## Outcome and current dependency

A session with hidden DevKit tools must receive matching host guidance and launch-time permission lists. Filtering tools/list is insufficient if the prompt still instructs the model to call those tools or lists them as pre-approved. H4b derives all three from the same prepared endpoint policy, with safety rules retained for every group set. It does not turn ordinary coding into a mandatory DevKit-tool workflow.

At preparation, #357 H4a is OPEN at reviewed head `76156886473f0a5acce4892d0f3ae1a8cde8ad09`, with fixes assigned to another agent for two P2 findings:

1. Old Close can unlink a replacement socket after the replacement has bound but before publishing its map entry. Bind/publication/cleanup need an incarnation-safe lifecycle boundary.
2. OpenWithPolicy validates names but accepts a policy registry with different BudgetExempt facts; the middleware can then exempt an ordinary tool. Host-owned fixed exemptions must stay authoritative.

Both have deterministic race-enabled reproductions from review. Do not treat that head as accepted. Pure guidance extraction/tests may proceed independently; real endpoint integration and an acceptance claim require the corrected H4a head, passing regression tests and review of its actual diff. The fix owner retains the H4a MCP lifecycle/budget files; do not implement those fixes again in this slice.

#353 is merged; #354's account creation fix and #355/#356 integration are now on main. Earlier statements that main was still `188706a8` are obsolete. Fetch at implementation time and verify the current main/PR ancestry; prefer main after corrected #357 merges, otherwise use a documented dependency branch. Work in a separate worktree and preserve all account/rewind/Close guards.

Read AGENTS.md, the subscription harness spec §§4–6, MCP tools guide, customization/recovery guide and vendors/sessions guide. This document is implementation guidance, not authority to change user policy, run paid model requests, message another session or merge PRs.

## Scope and file ownership

Primary files: `internal/agenthub/devkit_prompt.go`, prompt tests, OpenSpec/MCPEndpoint value types, `harness_runtime.go`, and the existing Claude/Codex/opencode adapter rendering call sites. Make only the minimal post-H4a change needed to return trusted served inventory through the endpoint contract. Preserve package boundaries: Agent Hub does not import MCP and `agentwork` stays a stdlib leaf.

H4c owns live S commit/publication, reconciliation/quarantine, Live=true, and list-change notifications. H4d owns production construction/activation acceptance. H5 owns public profile/proposal tools, controls and editor. H4b introduces none of those, no new approval queue, no account-selection changes, no UI navigation, and no production Harness activation in main.go.

## B1 — Inventory the current prompt contract before extraction

1. Inventory every host-authored section and its tool references in the current main, not this plan author's older worktree. Record each section's required groups and actual tools. Several current sections mix safety and workflow; split them by meaning rather than deleting a whole paragraph because one group is off.
2. Safety core includes untrusted data/output handling, no credentials/grant escalation, scope/path limits, environment confirmations and denied-confirmation finality, and branch/task guards. Preserve the current authorized semantics. An unavailable guarded action does not authorize a shell/CLI workaround. Do not restore stronger historical workflow requirements or turn optional tool discovery into a prerequisite to normal coding.
3. Classify multi-group workflows explicitly. Board/branch orchestration needs the applicable tasks/git/agenthub tools together; individual task read or child-control guidance can be a separate section where that avoids hiding useful served capabilities. Do not leave a section enabled merely because one of its groups is on.
4. Audit non-section references too: neutral/Codex/opencode naming notes currently use concrete git examples; the no-brief fallback calls workspace.list; PDF forwarding hints mention PDF tools. These must not advertise unavailable tools outside the filtered sections.
5. Audit all adapter paths: Claude new/load/fork metadata, Codex new/resume developer instructions, opencode prompt files, and any catalog/bootstrap use of OpenSpec. Catalog calls must not acquire a user session or mutate its policy to render a prompt.

Deliverable: a bounded mapping in code or focused documentation, with tests pinning actual tool references. Avoid another manually maintained registry of every tool name.

## B2 — Minimal immutable launch guidance input

1. Define a host-owned value contract sufficient to render guidance: prepared/legacy presence, actual registered-and-served DevKit tool names and any derived group availability. Reuse existing harness/endpoint values where appropriate; do not expose SDK types across package boundaries.
2. Distinguish **no prepared policy** from **a positively prepared empty tool set**. Nil/zero-value legacy behavior must not turn an all-groups-off session into the full prompt. A configured launch missing required policy/inventory facts fails preparation rather than falling back to full guidance.
3. Source this value from the same corrected H4a endpoint inventory/policy used to filter discovery and permission lists. Group-on alone is insufficient: conditional registrations, such as PDF support, can be absent. Never build guidance from a second read of current preferences, W, S, or a vendor environment report.
4. Copy/detach slices/maps/revision pointers before giving the value to an adapter or retaining it. A later input mutation, another session's policy or a live S update cannot modify an already prepared launch's text.
5. Carry only minimal trusted facts through OpenSpec and the shared H3 prepare path. Do not add a new resolver at each adapter. Protect workspace/session identity and endpoint incarnation; no caller-supplied arbitrary tool set grants availability or permissions.
6. Keep a trusted legacy path for callers without Harness (including current catalog reads). Do not manufacture a harness snapshot/ref or claim enforcement for that path.

Tests: absent versus configured-empty; conditional module absent; two sessions with different policies; mutation of source/returned values; missing required facts; one resolver/endpoint preparation per launch; registry mismatch remains refused.

## B3 — Render safety plus filtered guidance deterministically

1. Extract ordered section definitions with declared requirements and host-authored text. A section renders only when all its required groups/tools are actually served. Keep one source for section text, launch rendering and the future H4c replacement-guidance paragraph.
2. Keep the safety core independent of group switches. When git is off, retain a short fixed sentence explaining that the guarded action is unavailable and must not be bypassed through the shell; omit instructions naming the hidden MCP tools. Apply equivalent fixed guard wording where another section depends on unavailable services.
3. The safety rule may name CLI commands such as `git push`; these are not advertisements of an unavailable MCP function. Tests must distinguish CLI guard text from host-authored MCP tool references, not ban every occurrence of a group word.
4. Render vendor tool spelling only after selecting trusted sections. Use the existing Claude, Codex, opencode and neutral naming adapters. Generic invocation notes must use placeholders or examples that are actually served; if no DevKit tools are served, omit operational invocation examples.
5. When workspace.list is unavailable and no brief exists, use a fixed safe fallback that preserves known path/scope constraints and asks for missing context when necessary. Never instruct the model to call an absent tool.
6. Keep workspace instructions/brief and other untrusted inserted text separate from the host template renderer. Tool-looking text in a brief is data, not a new section declaration or a template command. Do not strip/rewrite user content to make a global tool-name test pass; test host-authored fragments separately and preserve the existing data boundary.
7. Preserve the legacy prompt byte-for-byte for the same legacy inputs where possible, with a captured fixture from the actual base. For a prepared policy, document intentional differences caused by real conditional registrations. Identical prepared inputs, brief and vendor spelling produce identical bytes/order without timestamps, map iteration randomness or volatile account/usage facts.
8. No rendered guidance may refer to an unregistered bootstrap tool. H1's reserved `harness.profile.propose` becomes usable only when H5 really registers it. Until then, unavailable capabilities direct the agent to the person; they do not invent a proposal call.
9. Provide a pure, bounded internal helper to render the replacement-guidance text for a specified newly enabled group. H4c may later attach it to a confirmed result. This helper performs no mutation, approval or notification and does not set Live=true.

Tests: every group off individually and all groups off; multi-group truth table; absent PDF registration; no-brief workspace-off; naming notes with git off; immutable safety core; all supported vendor spellings; untrusted brief containing tool placeholders; deterministic output; no fictional proposal tool.

## B4 — Align prompt and launch permission lists

1. H4a already narrows GateOwnedTools/PreApprovedTools to served tools. Preserve that distinction: gate-owned delegation still requires the host's gate after dispatch, whereas existing pre-approved reads have their own narrowly scoped contract. Never infer pre-approval from group-on or from text in the prompt.
2. Fill OpenSpec's DevKit guidance and GateOwnedMCP/PreApprovedMCP from one prepared value through H3's existing path. Intersect with actual registry facts and retain caller/workspace/env checks in their owners. Do not change unrelated third-party MCP servers or their permission policy.
3. Validate actual adapter wire arguments/metadata, not only an OpenSpec fixture: no hidden DevKit tool may appear in Claude allowed-tools delegation, Codex permission maps or opencode configuration. Safety hooks and their host-side refusals must remain active even when a workflow group is hidden.
4. Maintain launch-time semantics: prompt and vendor pre-approved lists do not change in place after S enables a group. Future H4c availability changes do not imply the vendor saw a refreshed list or newly pre-approved the tool. The replacement paragraph is guidance only; normal permission checks remain.
5. Cover create, Load/Open resume, reload, both rewind opens and account-driven reopen. Keep #353's retained executable baseline and atomic rewind reference, #354's actual selected account/generation, and PR343's writer admission fences. A rendering failure cannot publish a runtime or false applied snapshot.

Tests: real corrected SessionHost plus real resolver and fake vendor transports; tool-list/prompt/permission consistency across the lifecycle; no hidden tool delegation; default compatibility; explicit rejected host confirmation remains final; fresh children use their own prepared policy; runtime account facts are unaffected.

## B5 — Validation, documentation and delivery

Start with prompt/adapter tests and corrected H4a regression tests, then required checks on the final PR tree:

```sh
go test -race -timeout 40m ./internal/agenthub ./internal/mcp ./internal/harness ./internal/harnessconfig ./internal/app ./internal/bridge
go test ./internal/arch/
go test ./internal/... ./cmd/...
```

At plan preparation #357 reports a pre-existing race in `TestTasksWorkspaceScopedAccessMCP` (`fakeCaller.last`). Verify that against the actual base before treating it as unrelated. If it remains, disclose the exact failing baseline and isolate a separate fix; do not silently claim a complete race pass or suppress other failures. Run scoped vet/staticcheck, preserve tool-definition ceilings, and build real frontend assets before embedded composition checks. Run frontend tests/build and regenerate bindings only when corresponding source/signatures change.

Measure host-appended prompt bytes and serialized definition bytes for fixtures (all on, git off, board workflow unavailable, all off). Report those as bytes only, not tokens, quota, billing or evidence of vendor cache savings. No live vendor/model/account calls are required or authorized by this plan.

Update the focused MCP/customization guide with the implemented input contract, section requirements and fixed-at-open versus future live semantics. Keep activation inert until H4c/d readiness. Open one focused H4b PR through terminal gh; use the actual corrected H4a base/main and list any stacked dependency. Track the real H4 task/branch through MCP; do not overwrite another agent's branch metadata while it still owns the H4a fix. Use a separate subtask for H4b ownership when needed. Do not fabricate session ids, bypass verifiers, force-push, auto-merge, close H4, or start H4c in this PR.

Acceptance: actual endpoint policy, host guidance and vendor permission lists agree at launch; hard rules survive every group set; missing/empty facts cannot restore full guidance; no hidden/conditional tool is advertised by host-authored text; deterministic rendering and legacy behavior are tested; all lifecycle/account/executable/admission guards remain intact. H4a's two review fixes must be accepted before H4b integration is declared complete.

## Execution record

**H4b** (branch `harness/h4b-guidance`; the task branch metadata of OPT-79901a still names H4a). Built on `harness/h4a-serving` at 101802d9, which holds the two P2 fixes for #357 (bind/cleanup serialization, server-owned exemptions); retarget to `main` once #357 is merged. Delivered: `DevKitServing`, `MCPEndpoint.Served`/`OpenSpec.DevKit`, chunked guidance renderer with derived requirements, optional spans, fixed git guard, no-brief fallback, example-aware naming notes, brief inserted untouched, served-aware PDF guide and peer-reply line, `replacementGuidance`, wire tests for Claude/Codex/opencode, integration test over the real `SessionHost`. Found while inventorying: a brief used to be scanned for `{{tool}}` placeholders after insertion; it is now data.

Not done: H4c/H4d/H5; no production Harness; board delegation is hidden unless tasks, git and agenthub are all served (finer splitting left); the executable and account paths were not touched.
