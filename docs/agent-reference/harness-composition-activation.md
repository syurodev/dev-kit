# Harness composition and activation record (H4d)

Next workstream: [acceptance and production activation after H5b-2](../superpowers/plans/2026-10-27-harness-acceptance-activation-implementation.md).
This plan adds no acceptance evidence and does not change the blocked activation decision below.

Plan: `docs/superpowers/plans/2026-10-27-harness-h4d-composition-activation-implementation.md` (local planning
artifact, not on GitHub). Parent task OPT-79901a was **not** read or changed: no DevKit MCP tools were available
to the implementing session. This is engineering evidence, not an approval policy.

**Outcome: composition accepted, activation blocked.** `app.NewHarnessAssembly` builds the whole graph from narrow
inputs and `Attach` is proven through real SQLite, preferences, workspaces, the shared hub, the real gateway,
`SessionHost`, SDK clients and the usage collector, with only the vendor transports and the person's answers
scripted. **`main.go` does not call it: production Harness stays inert.** No flag, environment toggle or
partial wiring exists. This completes the composition sub-deliverable only; it is not H4 production completion.

## Dependency record

| Item | Evidence |
| --- | --- |
| Base | `origin/main` `89d8e88c` (#363), containing #357, #358, #359, #353 and #360 (`6befa5e2`). |
| H4c as merged | #360 merged with `golangci-lint` failing; #363 then fixed real defects (a Close that could clear S after a replacement reopened with the old override, launch checks that ran before the durable-reference/publication step, staged endpoints reporting `Live=true`, deprecated `HasTools`, unused declarations). #363's own CI was green; it states no complete nine-package race pass. The H4d tree re-ran the plan's race suite (PR body has the result). |
| Independent review | None beyond #363. H4c was authored and reviewed by the same line of work; this record does not count that as independent acceptance. |
| Production construction | None (`main.go` unchanged). |

## D1 — the constructor graph

Observed order in `main.go` (post-H4c, unchanged): SQL + migrations → `preferences`, `workspace` → `agenthub.NewService`
(vendors from the registry; Claude, Codex, opencode; server mode wraps them with `ForServerMode`) → shared
`interaction.Hub` (`Headless` in server mode) → `mcp.NewServerFromContainer` (gateway caller, usage scope/record
callbacks) → `mcp.SessionHost` → `agentSvc.MCPEndpoints = sessionHost` → UI starts. Shutdown: `sessionHost.Close()`,
`mcpCancel()`, usage collector drain (5 s), `agentSvc.Shutdown` (flushes transcripts; **not** a session Close, so
S is never cleared).

`app.NewHarnessAssembly(HarnessAssemblyConfig)` takes: the `HarnessStore` (injected as an interface because
`internal/app` may not import `internal/storage`), preferences (V and the writer boundary), the shared hub,
workspaces, Agent Hub, the vendor types the registry opens, and an `Inventory` func that must return the
tools the MCP server actually registered. It validates every collaborator (typed nils included), reads the
registry once (refusing an unreadable or empty one), and builds: provider, proposal service (real hub confirmer,
real V boundary, `harnessLive` over the H4c coordinator), snapshot adapter and lifecycle owner. Any failure returns a
fixed error and nothing is attached. `Attach` gives Agent Hub its provider and lifecycle owner **together**, only to
a non-nil host implementing `agenthub.PolicyEndpoints` (otherwise a configured session would run unfiltered or every launch
would refuse), only for the Agent Hub the graph was built for, and only once. No fallback sets `Harness=nil`. A launch with a pinned serving basis also requires a non-nil live-publication
handle from the endpoint (typed nils are refused). A launch-only host is closed and rejected before the vendor
starts, including for a default profile; it cannot leave a running endpoint outside S publication.

The final production change, **not applied**, would be after `agentSvc.MCPEndpoints = sessionHost` (checked to
compile under `-tags server`; it needs one added import, `devkit/internal/harness`):

```go
asm, err := app.NewHarnessAssembly(app.HarnessAssemblyConfig{
    Store: storage.NewHarnessStore(db), Preferences: container.Preferences, Hub: hub, Workspaces: container.Workspace,
    Agents: agentSvc, Vendors: agentReg.Types(),
    Inventory: func() (harness.Registry, error) { inv, err := mcpServer.Inventory(); return inv.Registry(), err },
})
if err == nil { err = asm.Attach(agentSvc) }
if err != nil { log.Fatal(err) }
```

## D2 — vendor and facts coverage

`harnessVendors` reads V for every vendor the registry opens (so opencode's stored customization and revision reach
the snapshot exactly) and refuses any other vendor with `harness.snapshotUnavailable` instead of returning an
empty V. `FactsComplete` stays **false** for every vendor (the environment report has names only), so group- and
budget-only proposals work under the explicitly incomplete-facts contract and anything that depends on
vendor-owned servers or skills is refused as facts-unavailable. Executable proposals remain unavailable (no
complete reader, no opt-in, no exact view). The customization read does not mean a vendor honors it: Claude
and Codex apply `spec.Customizations`; opencode's adapter does not today, so a stored opencode customization is
preserved but not enforced (unchanged behavior). No account, model, mode or sandbox change.

## D3/D4 evidence (all in `internal/app` unless stated; scripted vendors only)

| Row | Test |
| --- | --- |
| Incomplete graph / attach rules | `TestAnIncompleteGraphIsRefusedAndAttachesNothing`, `TestAttachNeedsAHostThatCanEnforceAndAttachesOnce`, `TestTheComposedGraphAttachesTogetherAndOnce` |
| Required live-publication endpoint | `TestPinnedHarnessRejectsEndpointsWithoutLivePublication`, `TestPinnedHarnessRejectsAHostDowngradeBeforeReload`, `TestPinnedHarnessLaunchOnlyReloadPreservesDurableState` |
| Vendor coverage | `TestTheVendorAdapterPreservesAnOpenCodeCustomization`, `TestAnUnsupportedVendorIsRefusedNotReadAsEmpty`, `TestAnOpenCodeCustomizationReachesTheLaunchUnchanged`, `TestAnUnsupportedVendorIsRefusedAtLaunchAndLeavesNothingBehind` |
| Fresh create, default compatibility | `TestAFreshDefaultSessionIsCoherentAndMatchesTheLegacyLaunch`, `TestAConfiguredWorkspaceNarrowsListingGuidanceAndPermissionsTogether` |
| Resume (Load and Open), reload, dead runtime | `TestResumeAfterRestartUsesLoadForAPromptedSessionAndOpenForANeverPromptedOne`, `TestAReloadThatCannotBePreparedLeavesTheRunningSessionAsItWas`, `TestADeadRuntimeIsRecoveredOnANewIncarnationThatKeepsS` |
| Account-driven reopen | `TestAnAccountMoveReopensThroughTheSharedPreparationAndKeepsS` |
| Both rewind opens | `TestBothRewindOpensServeTheDurableSAndKeepTheReference`, `TestARewindWhoseReferenceWriteFailsCutsNothingAndDiscardsTheStagedEndpoint` (INSERT/UPDATE/DELETE), `TestARewindWhoseSChangesWhileTheForkOpensIsRefusedAndCutsNothing` |
| Close/reopen, delete, subtree, child, shutdown | `TestCloseTombstonesSKeepsTheReferenceAndAReopenStartsWithoutIt`, `TestDeleteRemovesTheSessionAndALatePublicationCannotReviveIt`, `TestDeletingAParentRemovesTheSubtreeAndEveryHarnessRecord`, `TestAChildGetsItsOwnProfileAndNeverItsParentsS`, `TestShutdownIsNotACloseAndNeverErasesS` |
| Approval, hub, V boundary | `TestARealApprovalThroughTheSharedHubEnablesTheGroupLive`, `TestDenialHeadlessAndPromptlessHubsWriteAndPublishNothing`, `TestACustomizationEditThroughTheRealSetterStalesTheComposedProposal` |
| Gateway, grants, environment confirmation | `TestAGroupEnabledLiveStillObeysTheRealGatewayGrants`, `TestAGroupEnabledLiveStillAsksForEnvironmentConfirmation` (prod and an unknown tag ask every call, `allow_always` ignored; dev does not) |
| Error surfaces | `internal/bridge`: `TestHarnessLaunchRefusalsAreSurvivingPublicErrors`, `TestAHarnessRefusalNeverCarriesItsCause`. Launch/close refusals now publish `harness.snapshotUnavailable`, `harness.notEnforceable`, `harness.newSessionRequired` (`keptCodes`) and a fixed close-retry sentence; before this change they were redacted to "unexpected error". |
| Measurement and history | `TestMeasurementUsesTheEntrySnapshotAcrossAMidTurnChange`, `TestHistoricalAttributionIsNeverRewrittenByTheHarness` (real `agent_tool_usage_daily` and `agent_turns` rows byte-for-byte unchanged across refused change, failed launch, reconcile and Close), `TestARewindLeavesToolUsageHistoryAlone` |
| Terminal, server mode, optional modules, empty, no workspace | `TestTheUnprofiledTerminalSurfaceIsIndependentOfSessionPolicy`, `TestServerModeRefusesSessionsAndLeavesNothingBehind`, `TestAnUnwiredOptionalModuleIsNotRegisteredServedOrAdvertised`, `TestAConfiguredEmptyEndpointStillDiscoversALaterEnable`, `TestAPolicyResolvedAgainstAnotherRegistryIsRefusedByTheComposedHost`, `TestASessionWithNoWorkspaceStaysLegacyAndWritesNoReference` |

Install-failure and ambiguous-commit fault injection stays in `internal/mcp` (`serving_integration_test.go`): the
install hook is unexported and H4d does not add a production seam for it.

## D5 — activation checklist

| Gate | Status | Evidence / owner |
| --- | --- | --- |
| Accepted H4c | **Blocked (review)** | Merged, but it needed #363 after merge; no independent review. Owner: H4c author/reviewer. |
| Real registry | Pass | Assembly reads the server's actual inventory; `TestAnUnwiredOptionalModule…`, `TestAPolicyResolvedAgainstAnotherRegistry…`; size ceilings in `internal/mcp` unchanged. |
| Complete construction | Pass (tests) | The tests above. Not performed in `main.go` (decision below). |
| Vendor coverage | Pass for what the adapter does | opencode preserved; unsupported refused; `FactsComplete=false`. Real vendor processes were not run. |
| Approval and authorization | **Partial** | Hub, V boundary, grants and environment confirmation composed. Task-owner, branch and repository guards are handler-level and unchanged but **not** exercised through the composed graph. No new authority; executable proposals unavailable. |
| Lifecycle and recovery | Pass (scripted vendors) | Every D3 row above. |
| Live/status/discovery | Pass | `Live` needs the current exact acknowledgement; notification is never read as vendor refresh. **Vendor refresh after `listChanged` is unmeasured.** |
| Compatibility | Pass | No-W/S launch equals the legacy launch's permission lists; terminal surface independent; server mode refuses sessions and leaves nothing; no-workspace stays legacy. |
| Shutdown/error handling | Pass | `TestShutdownIsNotACloseAndNeverErasesS`; refusals fail closed (reload, unsupported vendor, Attach). |
| Full validation | **Blocked (partly)** | Backend, race, architecture and `-tags server` gates in the PR body. Local lint is unavailable (golangci-lint/staticcheck not installed; fetching one was refused), so lint/SA4000 evidence is CI-only. No native GUI or real-vendor run. |

**Decision.** Activation is not wired. Blockers, in the order they should clear: (1) an independent review of H4c as
merged; (2) a native, real-vendor smoke run of default sessions for Claude, Codex and opencode with the provider
attached (cannot be done in a headless session and is not claimed here); (3) CI lint on this PR; (4) composed
coverage of the task/branch/repository guards (or an explicit statement that they are out of the harness's reach);
(5) a product decision to attach the provider together with H5 rather than earlier: until H5 nothing can write W
or S, so attaching it adds launch-refusal and close-retry failure modes with no user-visible capability.

**If activated later**, the proven scope is: default (no W/S) sessions and internally applied group changes for
Claude, Codex and opencode, with `FactsComplete=false`, group/budget-only effects, no executable proposals, no
public get/propose transport, no editor and no policy setting.

## H5 handoff — internal seams

`HarnessAssembly.Service` (`harnessconfig.Service`): `Prepare(ctx, Request)` → `Prepared{ID, View}` (View has `Scope`
= `stored`/`running` changes and a two-section summary); `Cancel`; `Apply(ctx, id)` → `Result{Profile, Override,
Live, ReloadRequired, Commit, Publication}` and, after a commit whose publication failed, `*PublicationError` carrying
that Result (retry with `Reconcile`, never a new proposal); `Reconcile(ctx, sessionID)`. Callers must carry an
authenticated `identity` (an agent targets only its own session; the UI names a workspace and optionally a
session). Status: `agenthub.Service.HarnessStatus(ctx, sessionID)` (`State`, `Desired`, `Applied`, `Live`,
`Serving`) and `ServingTarget`. Revoke is `Patch.RevokeGroups` through the same Prepare/Apply (live-only effects
are judged under the launch's pinned W). Guidance after an enable: `agenthub.Service.ReplacementGuidance`.
H5a (below) delivered the group-only public get/propose tools, the grants, the group editor, the session controls and
the `PublicationError` mapping. Still later: budget editing and `headTail`, the auto-narrowing and executable policy
settings, executable proposals and the exact structured executable confirmation view.

## H5a — public group controls (integration record)

Base `origin/main` `b15aa541` (#368). Integration is complete and tested; **production activation is not wired and
stays blocked** (decision below). Integration and activation are separate outcomes.

**What exists.** One facade, `harnessconfig.Controls` (contract and bounded DTOs in `internal/harnessconfig/public.go`,
implementation `internal/app/harness_public.go`), serves the MCP tools and the window. Scope is never a request field:
an agent's session and workspace come from its authenticated identity and the persisted row, the window names a
workspace (and a session of it), and a terminal or unknown caller is refused in Go (`requireUI`, `agentScope`).

| Surface | Behavior |
| --- | --- |
| `harness.profile.get` | Read-only; follows its group policy like any tool. Per group: tool count, definition bytes (not tokens), W setting (`inherit`/`on`/`off`), S, saved and running availability (`unknown` is not `off`), session state. |
| `harness.profile.propose` | The one tool served when its group is off (`harness.ProposeTool`). No groups: discovery of what is off, nothing prepared, nobody asked. Groups: the person chooses **this session** or **workspace** through the shared hub (`KindHarnessScope`, a selection, not an approval), the final Request is prepared for that scope, then the existing `Apply` asks (`KindHarness`, `EnvBlocks`, no allow-always) and commits. Workspace scope commits W and the requester's S in one transaction; other sessions keep their pinned W. Declared gate-owned so the vendor does not prompt twice (see gates). |
| Window | `HarnessWorkspaceProfile/SessionProfile/PrepareWorkspaceGroups/PrepareRevoke/Apply/Cancel/Reconcile` (UI-only in Go). The editor patches `ProfilePatch.Groups` only; every other W field is untouched. A person-triggered change is still reviewed and asked. |
| Failure facts | A committed change whose publication failed, or whose commit is unknown, returns a typed `Outcome` (status `paused`/`unknown`, commit, publication, stamps, serving identity, `Live`, `ReloadRequired`, fixed recovery text) through the SDK as `isError` **with the same structured outcome**, and through the bridge as a normal return value. The cause never reaches it. Recovery is Reconcile, never a new proposal. |
| Public errors | `harness.profileDenied/profileInvalid/profileConflict/proposalStale/groupUnknown/factsUnavailable/notAvailable/noChange` are in `bridge.keptCodes` with fixed host sentences (`harnessControlRefusal`). |

**Construction order.** The tools register only when `mcp.Deps.Harness` is non-nil. That value is the startup-only
`app.HarnessPort`, created with the container (`Container.Harness`) **before** the MCP server, so both tools are in the
actual registry when `Inventory` freezes. `app.ComposeHarness(port, cfg, agents)` builds the graph from that
inventory, attaches Agent Hub, and binds the port once, or does none of it; before binding every operation refuses with
a fixed sentence. No second registry, late registration, global or flag. The window shows its entry points only from
`HarnessAvailable()` (whether the port is bound).

**Production is inert.** `main.go` neither passes `Harness` to the MCP server nor calls `ComposeHarness`; the port is
never bound, so the tools are not registered, `HarnessAvailable()` is false and no menu item, dialog or session card
appears. `TestProductionDoesNotWireTheHarnessYet` is a tripwire that must change together with the evidence below.

**What activation would change** (not applied): pass `Harness: container.Harness` in `mcp.Deps`; construct the session
host and set `agentSvc.MCPEndpoints` *before* starting `mcp.ServeUnix`; call `app.ComposeHarness(container.Harness,
cfg, agentSvc)` between the two and abort startup on error (today the listener starts first, so activation reorders
that block); then update the tripwire.

### A6 gates

| Gate | Status | Evidence / owner |
| --- | --- | --- |
| Independent review of the integrated H4c/#363/#367 invariants | **Partial** | AP1 completion review at `782a63c5`: `docs/reviews/2026-10-27-harness-ap1-completion.md`. Items 1-3 and 7 traced with no finding; items 4-6 traced in the main paths; item 8 has the #375 fixes, which the reviewer wrote and another reviewer must check. **Finding F-C (a stale group or tool name in a stored workspace profile blocks new sessions) now has a fix, below, that is not yet independently reviewed.** Reviewer is an AI session, not a human sign-off. |
| Composed guard tests through the controls | **Pass (scripted vendors)** | AP2 below: real Tasks and Git services, gateway, hub, SQLite and `SessionHost`, temporary repositories and local bare remotes. Earlier rows (grants, launch permission lists, prod and unknown-tag confirmation, identity matrix) are unchanged. |
| Real Claude, Codex and opencode runtime processes | **Not run** | No runtimes, accounts or mocked-model fixture here; scripted vendors only. No paid request was made. |
| Native desktop approval of the scope selection and confirmation | **Not run** | Headless host. The dialogs have SSR/state tests, not a real window. |
| `tools/list_changed` per runtime | **Unmeasured** | The outcome reports `DiscoveryReloadRequired` and Reload is the conservative fallback; no refresh is claimed. |
| Gate-owned delegation of `propose` on the real adapter wire | **Unverified** | Registered through `gateOwnedTool`; the service confirmation still always runs. The vendor wire permissions were not inspected. |
| Full validation | See the PR | Race over the nine packages, full Go suite, `go vet -tags server`, server build, architecture, frontend tests/build, Wails v3.0.0 bindings. Local lint is CI-only. |

**Decision.** Activation stays blocked until the first four rows above are real. After they are, the change is the small
`main.go` block described above plus the tripwire update.

### Evidence index (`internal/app` unless stated)

`TestThePublicToolsAreInTheInventoryBeforeItFreezesAndTheHandlerBindsOnce`, `TestWithoutTheControlsTheToolsAreNotRegisteredAndNothingIsReachable`,
`TestDiscoveryListsTheGroupsThatAreOffAndChangesNothing`, `TestTheAgentEnablesAGroupForItsSessionAfterChoosingAndApproving`,
`TestGuidanceNamesOnlyGroupsTheEndpointActuallyServes`, `TestSavingToTheWorkspaceCommitsWAndSTogetherAndLeavesOtherSessionsOnTheirPinnedW`,
`TestDecliningTheChoiceOrTheApprovalChangesNothing`, `TestADisconnectWhileChoosingLeavesNothingBehind`, `TestProposeIsServedWhenItsGroupIsOffAndGetIsNot`,
`TestOnlyTheAgentItselfCanAskAndOnlyTheWindowCanEdit`, `TestACommittedChangeWhoseEndpointCannotBePublishedKeepsItsFactsThroughTheSDKAndReconciles`,
`TestAnUnknownCommitIsNeitherARollbackNorASuccess`, `TestTheEditorChangesOnlyGroupEntriesAndKeepsEveryOtherFieldOfW`, `TestAStaleWorkspaceEditIsRefusedAndWritesNothing`,
`TestRevokingASessionGroupGoesThroughReviewAndApproval`; `internal/harnessconfig`: `TestOutcomeNeverClaimsMoreThanTheCommitAndTheEndpointProved`;
`internal/bridge`: `TestEveryRefusalOfThePublicControlsIsAPublishedFixedSentence`; frontend: `workspaceHarnessModel.test.ts`, `HarnessDialogs.test.tsx`.

Guidance after an enable covers **every** newly served group in one bounded text (`Service.ReplacementGuidanceFor`): sections
are whole, a section shared by two groups appears once, and a group that is hidden, unregistered (an unwired optional module) or
already served at launch contributes nothing (`TestGuidanceCoversEveryNewlyServedGroupOnce`,
`TestAnUnwiredOptionalGroupIsNeitherRequestableNorAdvertised`, `TestReplacementGuidanceForSeveralGroupsCoversEachOnceAndStaysWhole`).
The launch prompt and pre-approvals stay fixed; `listChanged` is still not read as a vendor refresh.

The session card's behavior is `toolGroupsController.ts`, driven in tests with deferred promises (`toolGroupsController.test.ts`):
every operation (read, prepare, apply, reconcile, reload) is bound to the session and epoch it started in, so a late answer for
a session the card left writes nothing into the new one and a late prepared proposal is withdrawn; an Apply in flight is not
cancelled by navigating away (that would close the open question). Discarding an answer is only about what the card shows: the
backend may have committed, and that is read back, never inferred. The card reads the backend again on an interval while the
window is visible, on focus and on this window's own apply/reconcile (`onHarnessChanged`); reads are single-flight and nothing is
optimistic (`Live` comes only from a read).

## H5b — result budgets, head-and-tail cuts and the narrowing opt-in (integration record)

Two stacked PRs on `origin/main` `7fd1dd21` (#370): the cutter and measurements (#371), then budget editing and the policy
(stacked on it). Integration only: **`main.go` is unchanged, the harness is still not activated**, and none of the A6 gates
above is waived. Executable proposals, vendor facts and `AllowExecutable` are untouched (always false).

| Area | Behavior |
| --- | --- |
| Cutter (#371) | `headTail` is a real cut of **plain text**; structured JSON, errors and fixed exemptions are never head-and-tail cut. Mode and size come from the call's one admission snapshot. The default/legacy/terminal path stays head. |
| Measurements (#371) | New rows are `text-utf8-v2` with the algorithm that actually ran (`none`/`head`/`headTail`/`structured`/`exempt`/`error`/`unrecorded`); v1 rows are never relabelled. Host text bytes only. |
| Partial edits | `BudgetEdit` (tool or group): size and cut each *omitted / set / reset*, translated over the current W in the shared preparation path; a rule left empty is removed; no other W field moves; exempt tools and all-exempt groups are refused; classification stays on the effective before and after (`Classify`), with exact numbers in the review (`results from <tool>: limit 16384 → 24576 bytes, cut head → headTail`). |
| Scope | A budget edit is a saved-workspace edit: **S still holds group names only**, the requester keeps its pinned limits until a reload, and an S enable after a budget edit resolves against the pinned launch basis (`TestAGroupEnabledAfterABudgetEditUsesThePinnedLaunchBasis`). |
| Reads | `ProfileWithBudgets` (agent, own workspace) and `WorkspaceBudgets` (window): bounded pages by tool, filter-bound cursors, explicit rules beside the effective value and its source (tool / group / default). |
| Agent form | `harness.profile.propose {budgets:[…]}`, exclusive with `groups`, workspace only (no scope question, no "this session"), no policy, approval, target or executable field in the schema (`TestTheAgentFormsCarryNoPolicyApprovalOrExecutableField`). No new tool, no new grant, no pre-approval. |
| Policy | `preferences.AgentHarnessPolicy{Revision, AutoNarrowing}`: default off, monotonic revision advanced by every change, CAS setter (`ErrHarnessPolicyConflict`), unreadable value reads as off. The setter takes the lock the final recheck and commit hold (`HoldAgentCustomization`) and is never held while a person decides. Window-only (`AgentHarnessPolicyGet/AutoNarrowingSet`, refused to agent and terminal in Go). `Policy.Revision` binds an approval, so a toggle — or off and on again — between Prepare and commit makes the proposal stale. |
| What the opt-in skips | Only an **agent's** pure **narrowing budget** edit's question. Window edits, widening, mixed, group/deny/server requests and executable tiers ask; headless and nil-prompter still refuse. |
| Window | "Result limits" tab in the lazy workspace dialog (`WorkspaceBudgetsPanel` + `BudgetsController`): KiB with exact bytes, independently resettable size and cut, exempt tools shown as never cut, a made-up sample illustrating the modes (not the cutter), drafts that survive conflict/read failure, stale-response protection and external refresh with the same epoch rules as the session card. Settings → Agents → Safety shows the opt-in only where the controls are wired. |

Evidence: `internal/mcp/result_headtail_test.go`, `internal/usagemetrics/cutmode_test.go`, `internal/harnessconfig/budget_test.go`,
`internal/preferences/harnesspolicy_test.go`, `internal/app/harness_budgets_test.go`, `internal/bridge/harness_policy_test.go`;
frontend `workspaceBudgetModel.test.ts`, `budgetsController.test.ts`, `HarnessDialogs.test.tsx`. Production wiring still
requires the A6 gates; `TestProductionDoesNotWireTheHarnessYet` is unchanged.

## Acceptance after H5b-2: AP1 review and AP2 composed guards

Base `origin/main` `e39d9060` (#372). Production is still inert: `main.go` is unchanged and
`TestProductionDoesNotWireTheHarnessYet` is intact. AP3 (real runtimes, native dialogs) and AP4 (activation) have **not**
been run; no gate below is marked passed on their behalf.

### AP1 - independent review (scope-limited)

Reviewer: an AI coding session that did not author H4c, #363, #367, #353 or H5. That makes it a different reviewer than
the author, not a human one; the owner should treat it as input to a sign-off, not the sign-off. Reviewed SHA:
`e39d9060d11bf7be0643098fe2739358b1c9b577` plus the two fixes in this change. Method: reading the code named below and
running probes through the composed rig; existing tests were run, not re-read.

| Plan item | Result | What was actually traced |
| --- | --- | --- |
| 1 Admission snapshot | No finding | `internal/mcp/serving.go`: one atomic load per call (`admit`), no store or vendor read on that path; quarantine/restore/install transitions under one lock; revoke affects later admissions only. |
| 2 Approval under pinned V/W | Partial | `internal/harnessconfig/apply.go` (`Apply`, `commit`): re-evaluation under the V boundary and session coordinator, W and digest comparison, policy revision in the binding. `liveEffectFor` and the pinned-basis arithmetic were not re-derived. |
| 3 Durable vs live | No finding | `commit`: `Live` only for `PublicationInstalled`; unknown commit reads stamps back and stays paused; a committed change with a failed publication returns `*PublicationError`. |
| 4 Endpoint fences | Partial | `internal/mcp/session_host.go`: incarnation per endpoint, close removes the socket/entry only while it owns it, under `bindMu`. The Agent Hub coordinator, reconcile and crash-between-commit-and-publish recovery were not traced beyond running their tests. |
| 5 #353/#363/#367 invariants | Not reviewed | `internal/agenthub/harness_runtime.go`, `harness_scope.go`, rewind and reopen paths: relied on the existing tests only. |
| 6 Public facade | Partial | `internal/app/harness_public.go`: `agentScope` (identity and persisted row), `requireUI`, `uiSession`. DTO bounds, cursors and fixed-error mapping not re-read. |
| 7 H5b | No finding | `internal/harness/classify.go`, `harnessconfig.evaluate` / `isBudgetOnly`: bounds on bytes, widening on any increase, the opt-in skips the question only for an agent's budget-only pure-narrowing edit. Observation: a cut-mode-only change is classified neutral narrowing, so under the opt-in an agent may switch head to headTail without a question; this matches the documented "pure narrowing" wording but is worth a product decision. |
| 8 No new authority | Two findings, fixed | See F1 and F2: the guard owners behind the harness-enabled groups did not hold in two cases. The harness itself added no tool, grant or pre-approval. |

Findings (both in code the harness does not own, reproduced through the harness-enabled `git` and `tasks` groups):

- **F1 (high) - git tools could act on a repository outside the workspace through a link.** `internal/mcp/git_repo.go`
  (`gitFor`): the folder, grant and nesting checks used lexical paths, so `repo: <granted folder>/link`, with `link` a
  symlink to another repository, passed as a granted workspace folder and `git.status`, `git.branch.create` and the other
  tools ran in the outside repository (its `HEAD` moved to the new branch). An agent can create such a link inside its own
  folder. Repro: `TestARepositoryOutsideTheWorkspaceIsRefusedHoweverItIsNamed` (fails on the base for the link cases),
  `TestScopeContainmentFollowsLinks`. Fix: containment compares resolved paths. **Follow-up (review of #375, concurrent
  case):** the first fix still opened Git at the caller's name, so a link repointed between the check and the open moved the
  operation (reproduced with real Git). The path is now resolved **once** per call (folders and granted roots are sealed once too);
  the scope decision, the candidate Git is opened at and the opened checkout's root check (`git.Service.Toplevel`) all use that
  single value, so a second review found no remaining double resolution (`TestALinkRepointedAfterTheSingleResolutionIsNeverFollowed`; `TestALinkRepointedBetweenTheCheckAndTheOpenDoesNotMoveTheOperation`,
  `TestGitIsOpenedAtTheCheckedPathNotTheCallersName`, `TestARepositoryThatIsNotTheOneCheckedIsRefused`). **Remaining limit:** the checks
  are path-based; an agent that swaps a real directory component of the checked path for a link after the open can still redirect
  later commands. Closing that needs descriptor-based execution in `internal/git`, which is not done. Not changed:
  `workspace.list`'s informational `granted` flag is still lexical.
- **F2 (medium) - an agent could release or re-aim another live session's task claim.** `internal/tasks/transition.go`
  (`Release`) and `service.go` (`SetFields` branch): `Claim` refuses a live holder, but `Release` and the `branch` field did
  not, so `tasks.release` then `tasks.claim` took a live session's task, and `tasks.setFields {branch}` changed what its holder
  may push. Repro: `TestATaskHeldByAnotherLiveSessionIsNotTakenDroppedOrReaimed`, `TestAnAgentDoesNotReleaseOrReaimTheLiveClaimOfAnotherSession`.
  Fix: both refuse with `ErrClaimed` unless the caller holds the task, the holder is dead (as `Claim`), or the caller is the
  person. **Follow-up (review of #375, concurrent case):** the holder check and the metadata write were separate steps, so a
  `Release` that found the holder dead could erase a claim another session took meanwhile (`TestAReleaseDoesNotEraseAClaimTakenWhileItDecided`,
  a barrier test that fails without the lock). Task metadata writes (`mutateAs`) now hold one write lock across read, check and
  write, and `Claim`/`Assign` re-apply the holder rule to the metadata read under it (`TestTwoConcurrentClaimsOfAFreeTaskLeaveOneHolder`).
  The lock is per process, which matches the single-process local store. This tightens behavior; it is in its own commit so the owner can drop or adjust it. Not changed or reviewed:
  `Transition`, `Update`, `Archive` and comments by a non-holder on a held task.

Not reviewed at all: the frontend, the budget cutter, usage metrics, accounts, the vendor adapters, and anything in AP3/AP4.

### AP2 - composed Tasks/Git guards through the controls

Rig: `harness_composition_work_rig_test.go` (real `tasks.Service` with workspace scope and Agent Hub liveness, real `git.Service` with
the application's guard store, environment policy and hub confirmer, `NewTaskBranches`, `mcp.GitReposOf`); tests in
`harness_composition_guards_test.go`. Temporary repositories, local bare remotes, an isolated `HOME`/Git config. Only the vendor
transport and the person's answers are scripted. Each refusal is checked against the repository refs and work tree, the task's
durable binding and the number of questions put to the person.

| Plan row | Test |
| --- | --- |
| Group off | `TestHiddenGitAndTaskGroupsAreRefusedBeforeTheirHandlers` |
| Enabled by public proposal; existing grant still required | `TestEnablingTheGroupsByAProposalMakesNoClaimAndAddsNoPushRight`, `TestAnEnabledGitGroupStillNeedsTheGrantsOfItsTools` |
| Ownership absent or wrong | the two above, `TestATaskHeldByAnotherLiveSessionIsNotTakenDroppedOrReaimed` |
| Branch guard | `TestAGuardedBranchAsksForEveryMutationAndADenialChangesNothing` (commit, amend, push; deny, allow, allow_always, unknown and non-confirming tags) |
| Push/task binding and positive control | `TestPushNeedsABranchRecordedOnATaskTheCallerHolds` (default, unowned, another session's, task naming the default branch, missing remote, released task; the owner's push lands on the bare remote) |
| Repository outside the workspace | `TestARepositoryOutsideTheWorkspaceIsRefusedHoweverItIsNamed` (absolute, relative, linked worktree, links) |
| Read-only workspace folder | `TestAWorkspaceFolderTheSessionMayNotWriteIsReadOnly` (the same folder granted at creation writes) |
| Corrupt profile is not an empty profile | `TestACorruptStoredProfileRefusesANewSessionAndNeverServesUnfiltered` (new session); reload was already covered |
| Terminal/agent/window identity | unchanged (`TestOnlyTheAgentItselfCanAskAndOnlyTheWindowCanEdit`, `TestTheUnprofiledTerminalSurfaceIsIndependentOfSessionPolicy`); no new case added for Tasks/Git |

Gaps: squash, rebase, cherry-pick and worktree guard gates were not each exercised through the controls (commit, amend and push
were); a shell fallback after a denial cannot be asserted here (the sandbox owns that); the post-H5b budget cases listed in the plan
were already covered by `harness_budgets_test.go` and `harness_composition_*_test.go` and were not duplicated.

### Validation (Go 1.27.2, darwin/arm64, Bun, Wails not needed: no AppService change)

Run 2026-10-27 on `harness/ap2-composed-guards`. The first run covered the earlier head; after the review of #375 the race
suite (tasks, git, mcp, app, bridge, agenthub, harness, harnessconfig, preferences, interaction, usagemetrics, storage: 12 packages,
no race reports), `go test ./internal/... ./cmd/... -count=1` and `go vet -tags server ./...` were re-run on the fixed head (after the single-resolution change) and pass.
The frontend and `internal/arch` rows below are from the first run (no frontend or import change since):

| Check | Result |
| --- | --- |
| `bun install --frozen-lockfile`, `bun test --isolate`, `bun run build` | pass (5119 tests, 0 fail; build ok) |
| `go test -race -timeout 40m` over harness, harnessconfig, agenthub, mcp, app, preferences, interaction, usagemetrics, storage, bridge | pass, no race reports |
| `go test ./internal/arch/` | pass (after building `frontend/dist`; before it, `go list` fails on the embed pattern) |
| `go test ./internal/... ./cmd/... -count=1` | pass |
| `go vet -tags server ./...`, `go build -tags server` | pass |
| `staticcheck` on app, mcp, tasks | 4 findings, all in files this change does not touch (`agentbrief.go:94`, `app_test.go:80`, `plantasks_test.go:78`, `tools_pdf.go:385`); not checked against the base |
| `golangci-lint` | **not run**: the installed binary was built with an older Go and fails to typecheck against Go 1.27.2 (export data version). CI lint remains the evidence. |

The CRLF Git test failure and the `fakeCaller` race mentioned in earlier reports did not occur in these runs. Linker warnings about the macOS deployment target are build noise.
Not run, and not claimed: AP3 (real Claude/Codex/opencode processes, native approval dialogs, per-runtime `listChanged` measurement)
and AP4 (activation). The A6 rows for those remain **Not run / Unmeasured**.

### AP1 completion (2026-10-27)

Report, ledger per parent item, findings and limits: [`docs/reviews/2026-10-27-harness-ap1-completion.md`](../reviews/2026-10-27-harness-ap1-completion.md). AP1 remains **Partial**. Open for activation:
F-C (no repair path for a stale stored profile, owner decision), a reviewer other than the author for the #375 git-scope and claim-lock code, and the AP4 decision on sessions that have no
applied reference (O-2). AP3 and AP4 are still **not run**; production is still inert.

### F-C repair: stale references in a stored workspace profile

Fix commits: `dd8e8150`, `d3aceead`, `952908e1`, `1e94743e` (base `eccc7c6c`). The historical failing evidence (AP1 report, repro `.go.txt`) is kept as it was.
**Not independently reviewed:** the same session reviewed AP1 and wrote this; another reviewer must read it before AP1 can move past Partial.

Behavior. The window can diagnose a workspace's stored profile (`StaleProfile`) with no session and even when the ordinary read fails, and can prepare a repair
(`PrepareStaleRepair`) that removes only the keys of `W.ToolGroups`, `W.ResultBudget.Tools` and `W.ResultBudget.Groups` that the server's registry does not have. The
caller names only the workspace; Go derives the removals. States: `none` (no profile, or nothing stale and it resolves), `stale` (repairable), `unrepairable` (corrupt record,
unsupported schema, another invalid setting such as a budget on a never-cut tool, an unavailable registry). More than 64 stale keys, or fewer whose review would exceed the 8 KiB confirmation summary, are shown (in part for the first case) with `truncated` set and cannot be prepared; the window explains this and offers no repair.
The normal resolver is unchanged and still strict.

Approval. The repair goes through the existing proposal service: prepared-record ownership, the shared hub confirmation (tier `widening`, always asked, `EnvBlocks`, so
no allow-always and **auto-narrowing is never consulted**), the writer boundary and the store compare-and-swap. The prepared repair is bound to the exact profile and revision, the
registry identity (tool names and cut exemptions), every vendor's customization revision, the policy revision and the repaired body; Apply scans again under the boundary
and requires the same binding (`ErrConflict` if W moved, `ErrStale` otherwise). The review lists every removal with its saved value and states that a removed "off" is forgotten,
removed limits are not restored, running sessions keep their basis, nothing is reloaded, and that restoring the module or build is the alternative. One write advances W's
revision once; S, running endpoints, applied references, executable guards, usage history and accounts are not touched, and repair approval is not executable approval.
An ambiguous commit is read back (`Expect+1`, `Expect` or unknown) and reported as uncertain with its own wording; no session reconcile is offered and the write is not retried.

Surfaces. Window only: `HarnessStaleProfile`, `HarnessPrepareStaleRepair` on the bridge (existing `harness:read ui:profile` and `harness:write ui:change` grants; the prepared id is applied
and cancelled with `HarnessApply`/`HarnessCancel`). Agents, the terminal and unknown origins are refused in Go; no MCP tool, schema field or capability was added; no new error code
(the two new refusals reuse published codes and are in the published-refusals test). The workspace harness dialog shows the repair view in place of the group editor while a repair is on
offer; a failed read or an unrepairable profile never offers one. The misleading "Repair or reset it in Settings" text was corrected; there is still no Settings entry.

Evidence (unit, composed, UI). `internal/harnessconfig/repair_test.go`: the pure helper, UI-only identity, every unrepairable case, the 64-entry bound, hostile names, exact review text, one revision step,
deny/cancel/replay/stolen id, headless, auto-narrowing on, W/vendor/policy/registry changes while the question is open (each fails when its check is removed), ambiguous commits.
`internal/app/harness_stale_repair_test.go`: the report's pdf scenario end to end (launch fails, diagnosis, denial changes nothing, approval, new session starts, module restored uses the default),
every kind of key with known fields kept, healthy/absent, corrupt, identity and absence of any tool, staleness, always-asks and headless, S/endpoint/reference/usage untouched, ambiguous commit.
`frontend/src/workbench/staleRepairController.test.ts` and `HarnessDialogs.test.tsx`: reachable offer, no offer on read failure, epoch fencing and navigation, cancel, conflict, saved and uncertain outcomes,
single submission, exact listing. Review of #377 found (P2) that a list near the 64-entry bound overflowed the dialog's `85vh` limit and pushed Review repair / Repair out of view;
the content now scrolls in a bounded region with the actions outside it (SSR structure tests with 64 entries fail on the old layout). Controller/SSR tests are code-level evidence; the real dialog's scrolling, focus and viewport are not run and remain AP3.

Validation (Go 1.27.2, Bun 1.4.2; bindings regenerated with the installed `wails3` v3.0.0-beta.32, because `go run` of the pinned v3.0.0 CLI needs go.sum entries this change should not add; the diff is additions only):
`bun test --isolate` 5141 pass and `bun run build` passes after the layout fix (the build first failed on the tests' enum typing at `1e94743e`, fixed); `go test -race` over the ten plan packages, `go test ./internal/arch/`, `go test ./internal/... ./cmd/...`,
`go vet -tags server ./...`, `go build -tags server` pass. The race run began before one line was added to a bridge test list; that package was re-run without `-race` and passes. `staticcheck` shows the same three findings
in files this change does not touch. `golangci-lint` cannot run locally (built with an older Go); CI lint applies.

Limits. Out of scope and unchanged: O-2 (sessions with no applied reference), AP3/AP4, the directory-swap limit from #375, repair of corrupt records, a Settings entry, any automatic migration or reset. Only the cases above are repairable.

### Stale session override (R1a-1, 2026-10-28)

F-C covers W only. A session override (S) that names a group the build no longer registers cannot resume, and the window's revoke could not evaluate it. The window's Revoke now accepts such a name
(`harnessControls.PrepareRevoke`) and prepares `Service.PrepareSessionRepair` (`internal/harnessconfig/repair_session.go`): UI-only in Go, removal only, every stale group derived from the stored S and the registry
(the sent name only selects and must be one of them; a registered group is never removed this way), always confirmed (`TargetSessionRepair`, labelled narrowing, hub `EnvBlocks`), bound to the S and W stamps, the registry,
the vendor and policy revisions, one S compare-and-swap under the session's coordinator, spent on use, ambiguous commits read back. A session with a running endpoint is refused. The resolver stays strict;
`harness.ErrStaleSessionOverride` (published code `harness.snapshotUnavailable`, fixed sentence, no stored name) replaces the generic resume refusal only when S is the stale part and W has none.
If W is stale too, repair W first. Tests: `internal/app/harness_stale_session_repair_test.go`, `internal/harnessconfig/repair_session_test.go`. Spec: `docs/superpowers/specs/2026-10-28-stale-session-override-repair.md`.
Not independently reviewed; the native card is AP3.

### AP1 R1 closeout (2026-10-27, base `c2d3074f`)

Full ledger, repros and the row table: [`docs/reviews/2026-10-27-harness-ap1-completion.md`](../reviews/2026-10-27-harness-ap1-completion.md) ("R1 closeout (second pass)"). Production is still inert; AP3 and AP4 are **not run**.
AP1 stays **Partial**. Not independently reviewed: every fix below, the #375 git-scope and claim-lock code, and the F-C backend at its final head were written by the reviewing session.

| Finding | Severity | Fix (in this PR) | Independently reviewed |
| --- | --- | --- | --- |
| R-1: a rewind closed the running session before the launch gate could refuse the fork | medium | `78bfd1e5` (`preflightHarness` before step 2) | no |
| R-2: an account move did the same and put the target account on cooldown | medium | `908d7d71` (same preflight before `moveSession`) | no |
| O-1: files restored before the stale-launch check | low-medium | `786440e0` (`checkServingLaunch` before the restore) and `c9b7543e` (an S change during the restore is finished forward: discard the staged fork and endpoint, prepare again by the ordinary rules, no second restore, at most 3 attempts; anything else, or running out, returns `RewindPartialError` saying the files were restored and the conversation was not) | no |
| R-3: `tasks.OnChanged` ran under `writeMu` (from #375) | low, latent | `daa43bf5` | no |
| F-D: the shared dialog body overflows the card padding; action rows sit 16 px below the card (also on the existing editor footer) | low, pre-existing | not fixed (shared component, owner) | - |

Evidence added: a probe that a crash between the S commit and its publication is recovered by the next launch; a real-browser layout check of the repair dialog at the final #377 head
(64-entry diagnosis and review scroll in a bounded region, actions on screen, Cancel focused, long names wrap; scratch page, not the native window).

Decisions prepared for the owner (details in the report): **O-2** - accept for AP4 on the precondition that no product path writes workspace executables, and add the conservative
"no reference + workspace-owned executable => new session required" rule before any such path ships; no baseline is stamped from the current profile. **Directory swap (#375)** - an open,
pre-existing limit of the shipped Git tools, not introduced or relied on by the harness, so **not an AP4 blocker**; it must not be described as a hard boundary against a hostile agent, and
closing it needs a descriptor-bound execution design with owner review.

Still required before AP1 can be marked complete: a different reviewer for the fixes above, the #375 code and the F-C backend; the O-2 decision; the owner's call on the directory-swap design and F-D; then AP3 (real runtimes, native dialogs) and AP4.

### O-1 finish-forward hardening (2026-10-28, base `87159250`)

`c9b7543e` (above) already put a bounded finish-forward on main; the plan `2026-10-27-harness-rewind-finish-forward-implementation.md` was written as if it were not there. This delivery checked that
code against the plan, test first, and fixed what the checks found. Code and tests: `7e4dbe98` (backend, bridge) and `6f182c85` (frontend). Production is inert; AP3 and AP4 were **not run**; AP1 stays **Partial**; none of this is independently reviewed.

| # | Found by a failing test | Fix |
| --- | --- | --- |
| 1 | S moved **and** the staged endpoint was lost: the rewind retried as if only S had moved (`TestAnSChangeTogetherWithALostEndpointIsNotRetriedAsS` returned success) | The coordinator decides the reason at the refused publication. Order, most final first: Close in progress or failed, a finished Close (new `servingCoord.lifecycle`, moved by Close only), a lost or replaced endpoint, S (the epoch). Only the last is retried. The session row is still re-read as a second word |
| 2 | `prepareLaunch` kept its own `launchAttempts` per round: a rewind could prepare 4 launches (`TestEveryPreparationOfARewindSharesOneLaunchBudget`) | One budget per rewind, `prepareLaunchWithin`: the first launch and every finish-forward round draw from the same `launchAttempts` (3). Upper bound: 3 launches/endpoints; forks 3 x (1 + the vendor's fallback candidates, which stay their own finite loop) |
| 3 | `publicError` read the cause chain first, so a partial rewind caused by a cancelled caller reached the window as "Operation cancelled." and one caused by a locked vault as "Vault is locked" (`TestAPartialRewindIsNeverReportedAsTheCauseBehindIt`) | `rewindOutcomeRefusal` is the first check. Two published codes, both fixed sentences with no cause, both in `keptCodes`: `agenthub.rewindFilesRestored` (proven not cut) and `agenthub.rewindOutcomeUnknown` |
| 4 | A failed `Commit` was treated as "not committed": the fork was deleted and "the conversation was not rewound" was claimed | `RewindSession` wraps a commit failure in `ErrRewindCommitUnknown`. The rewind reads back (the session's vendor id, else the safety row, both written by the same transaction) on a bounded host context. Committed: finished as a success, publication only, no second cut. Not committed: the partial outcome. Unreadable: `RewindUnknownError`, the fork is closed and never deleted, the source stays, the safety row stays pending |
| 5 | A cancelled caller left the safety row pending and the forks undeleted (`TestACancelledCallerStillSettlesTheSafetyRowAndDiscardsTheForks`) | Safety bookkeeping, fork discard and the read-back run on `hostContext` (15 s, no caller cancellation). A bookkeeping write that fails is logged and reported through `OnRecordError`; the row is left as it is, not claimed settled |
| 6 | `RewindDialog` showed "Nothing was changed." for any non-Error and left the spent plan's button on | `attemptRewind`/`rewindFailure` sort a failure by the published code; a partial or unconfirmed outcome keeps its own warning, clears the plan, never calls `onRewound` |

Evidence (real SQL, real git checkpoints, scripted vendors, no network): `internal/agenthub/harness_rewind_files_test.go` (restore once, one safety row, bounded rounds, lost endpoint, budget, ambiguous commit in
three forms and a conversation-only one, cancelled caller, unsettleable row, crash during the retry replays nothing, gate refusal on a changed executable or an unreadable snapshot, endpoint staging failure,
winner announced once and the source deleted once); `serving_live_internal_test.go` (reason precedence); `internal/bridge/errors_harness_test.go` (the funnel, and the payload as `AppService.call` and `json.Marshal`
hand it to Wails: code, message, recoverable and nothing else); `frontend/src/modules/agenthub/rewind.test.ts` (the bridge payload through `toAppError`).
`internal/app/harness_composition_rewind_files_test.go` is the primary acceptance case: an S change **committed through the real harness configuration service, MCP session host and coordinator** after the files were
restored (`OnFilesRestored`, which fires inside the restore under the rewind's claim) finishes the rewind: one restore, two forks, the winning launch resolved the new S (the revoked group is not served, the
stored reference carries its fingerprint), cut and reference together; and on every attempt it ends in the partial outcome with 3 forks, one restore and the safety row kept. With the retry switched off the first test fails with the original O-1 symptom.
The agenthub tests move S with an epoch-only seam (`MarkPending`); the committed path is the app test's.

Validation (Go 1.27.1, linux/amd64, headless, no Wails GUI): see the PR. Limits that remain:

- **A restore that stops part-way (2026-10-28).** `checkpoint.ApplyRestore` marks a failure that came after the folder began to change (`checkpoint.ChangedError`). The rewind then returns `RewindPartialError{RestoreIncomplete: true}` (same published code `agenthub.rewindFilesRestored`, its own fixed sentence: "only partly put back"), discards the fork and keeps the rewind's safety row **pending**, so `InterruptedRewinds` lists it. A restore refused before its first write stays the plain refusal and the row is settled `failed`. After the cut commits, the mode/model re-application and the source-session deletion run on `hostContext`. See `docs/superpowers/specs/2026-10-28-rewind-partial-restore-honesty.md`.
- **Not atomic.** Files and SQL are not one transaction. A crash after the restore and before the cut leaves a pending `rewind_safety` row (listed by `InterruptedRewinds`); nothing is replayed or restored again by the host.
- **Window control for recovery (2026-10-28, partial).** `InterruptedRewindNotice` (`frontend/src/modules/agenthub/surfaces/`, mounted in the Agent Hub content and the rail) shows only while the session has a **pending** `rewind_safety` row, read from the session's own `AgentHubListCheckpoints` (`InterruptedRewinds` has no session id and is not target-authorised). It says the files may be put back or only partly, that the conversation was not rewound (a pending row proves the cut did not commit: both are one transaction), and that a copy is kept. It offers exactly two things: put the files back through the existing `undoRestore` preview, token and overwrite confirmation (`UndoRewindFilesDialog`), or settle the row without touching a file (`AgentHubDismissInterruptedRewind`, after a confirmation, since it ends the only way back from the window). A successful undo does not settle the row (the backend does not); the notice stays until the person closes it. Nothing repeats a restore or completes the rewind. **Not covered:** a rewind whose files were restored and whose cut was then refused (S moved, budget exhausted, refused publication) settles its row `failed`, so it raises no notice and has no window control; the public sentence "rewinding the same message again can finish it" remains the only path for it. Settling those rows differently is a backend decision. The public sentences are unchanged for that reason. Spec: `docs/superpowers/specs/2026-10-28-rewind-recovery-ui.md`.
- **Seams.** The agenthub test hook fires inside the restore before the files are written (restore-safety row insert); the app test fires after they are written. Neither interleaves with the middle of a file write, which S does not touch.
- **Not exercised:** a session deleted between the restore and the retry (read as final: the session row cannot be read); a committed cut whose publication the fence then refuses (the session has no live runtime and resumes from the stored conversation; the old vendor session is left, not deleted); a real vendor, a native window, account switching.


### R1 independent reviews and the owner's decisions (2026-10-28, base `8c5e64d0`)

Two reviews by other instances of the same model family (not a human sign-off; neither wrote the code it read):
[R1a](../reviews/2026-10-28-r1a-fc-o2-dirswap-review.md) (F-C #377, O-2, the #375 directory-swap boundary) and
[R1b](../reviews/2026-10-28-r1b-376-items-and-379-review.md) (the merged O-1 finish-forward #379 and ledger items 4-8 of #376).
R2 (real-runtime protocol evidence, [report](../reviews/2026-10-28-r2-real-runtime-evidence.md)) is merged. **AP1 stays Partial; AP3 (R3, native)
and AP4 (activation) are not run; production is still inert.** Nothing below is an acceptance.

**What now has independent evidence.** F-C backend and UI (traced, focused tests run, a real headless-browser layout check of the repair dialog; not the
native window). #379: retry classification, the shared three-launch budget, ambiguous-commit read-back, bridge sentences and the frontend outcome
sorting held up under reading and four mutations. Ledger items 4, 5 and 6 (item 6 with a probe test that every harness setter and budget read refuses a
caller that is not the window). Item 7 only in part; item 8: the comments on #375 are a bot review and the author's own replies, so they are **not**
used as an independent sign-off; the Git canonical-path binding and Claim/Release serialization were reviewed on their own and no defect was found.

**Decisions of the owner (recorded 2026-10-28).**
- **O-2 (sessions with no applied reference): the reviewer's recommendation is accepted.** AP4 may proceed on O-2 only on two conditions, both now
  activation conditions: (i) no product path writes workspace executables (`Servers`, `SkillFolders`); the one unconditional refusal
  (`ErrExecutableNotReady`) stays; (ii) **Option B with E** is a hard prerequisite of AP4 and of any change that lets W carry executables: on any reopen
  (resume, reload, rewind forks, account move and their preflights) with no stored reference and a snapshot that has a workspace-owned executable,
  refuse with `ErrHarnessNewSessionRequired`, reading only, writing no row; `HarnessStatus` shows it before the launch refuses. Create is unchanged.
  Option C (stamp a baseline from today's W) is rejected. **Not yet decided:** whether an inert build should keep deleting existing references
  (`harness_runtime.go`, `rewind_store.go`); it stays as it is until the owner decides, and B makes it less urgent.
  **Implemented (condition ii, 2026-10-28, branch `feat/harness-o2-reopen-gate`, not independently reviewed):** `harnessGate` takes a `launchKind`
  (`launchCreate` or `launchReopen`); with `previous == nil`, a reopen and `len(snap.Ref.WorkspaceOwned) > 0` it returns `ErrHarnessNewSessionRequired`.
  `CreateSession` (`openNewRuntime`) passes `launchCreate`; resume/reload/account reopen (`resumeRuntime`), the rewind forks (`openForked`) and the
  three preflights (`preflightHarness`, always a reopen) pass `launchReopen`. It reads only (no row, no baseline), an unreadable row is still
  `ErrHarnessUnavailable`, and `HarnessStatus` maps "no reference and W owns an executable" to `new_session_required` (the window already shows it via
  `SessionState.newSessionRequired`). Condition (i) still holds and the inert-build deletion question is **still open and unchanged**. Tests:
  `internal/agenthub/harness_o2_reopen_test.go`.
- **The Git directory-swap and `.git` indirection (#375): treated as a release-security blocker, and being fixed.** The Git scope must not be described
  as a boundary against a hostile agent until it is. Three routes reach a repository outside the grant (two need no race) and one reaches host execution.
  Under the plan's literal criterion this is not an AP4 blocker (the Git group is on by default today and activation only adds an off switch), but the
  owner chose to fix it now. Delivered in stages: the `.git`-file redirect and repository-supplied execution for agent-origin calls first, then
  descriptor-bound execution and guards keyed on verified identity (stage 2: `docs/superpowers/specs/2026-10-28-git-descriptor-bound-execution.md`;
  it narrows the swap, and a few microseconds of git's own work-tree `chdir` by name remain). Links to the PRs are in the commit history of each stage.
- **Findings the owner chose to fix before R4:** R1a-1 (a session override S naming a group that is no longer registered locks the session out and the
  window's revoke fails with `harness.groupUnknown`) and R1b F1 (a file restore that fails part-way is reported as a plain refusal and its safety row
  is marked `failed`, so `InterruptedRewinds` never lists it) with F2 (two retry-budget tests that hang and exhaust memory instead of failing when the
  budget is broken; that, not a defect on `main`, was the 16 GB process that crashed the machine).

**Open, not scheduled.** R1a-2 (the 64-entry repair bound is unreachable under the 8 KiB summary cap and the refusal says "split it" for a repair),
R1a-3 (the Result limits tab can be closed while a repair apply waits for confirmation), R1a-4 (F-D, the action row hangs 16 px below the card at
maximum height), R1b F3 and F4 (post-commit cleanup uses the caller's context; not reproduced), F5 (one test pins the retry reason on its own), and item 7
(`usagemetrics` and the budgets panel internals were not read). The window control for interrupted rewinds covers pending rows only (see the O-1 record above).
**Fixed after R1a (2026-10-28, not independently reviewed):** R1a-2, R1a-3 and R1a-4, with a real headless-browser layout measurement but not the native window
([spec](../superpowers/specs/2026-10-28-repair-dialog-lows.md)). The diagnosis reports a repair as not offered when its review would not fit the 8 KiB summary
(the 64-entry bound is only an upper bound), and the refusal no longer says "split it"; every way out of the dialog, including the Result limits tab's Close, shares
one busy rule; the dialog body shrinks inside the card so the action row no longer hangs below it (the same shared-component hang remains in other dialogs).

**Open, not scheduled.** R1b F3 and F4 (post-commit cleanup uses the caller's context; not reproduced), F5 (one test pins the retry reason on its own), and item 7
(`usagemetrics` and the budgets panel internals were not read). The window still has no control for interrupted rewinds (see the O-1 record above).
Not run by either reviewer: a real jail, macOS, the native window, whole-package `-race`, lint, `bun run build`.

## R3 native evidence (accepted by the owner with rows open, 2026-10-28)

[`docs/reviews/2026-10-28-r3-native-evidence.md`](../reviews/2026-10-28-r3-native-evidence.md) records the native and server-mode rows run on an
acceptance-only candidate (unmerged, local branch `candidate/harness-ap3-native`, head `384a95d4`) with a scripted peer in a disposable data root. The owner accepted it as R3 evidence on 2026-10-28 with open rows. It is
**partial**: the approval, revoke, auto-narrowing, terminal-identity and server-mode rows have evidence; stale repair, restart, the exact budget review in
the window and the late-approval variants were not run, and a failed publication cannot be produced natively. The O-2 decision and the ordinary review of the R4 pull request are still required. It also
records a window-blanking render error reachable from the prompt box (present on `main`, not harness code) and the candidate's startup-behaviour
differences R4 must decide.
