# Harness composition and activation record (H4d)

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
unsupported schema, another invalid setting such as a budget on a never-cut tool, an unavailable registry). More than 64 stale keys are shown in part and cannot be prepared.
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
