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
a host implementing `agenthub.PolicyEndpoints` (otherwise a configured session would run unfiltered or every launch
would refuse), only for the Agent Hub the graph was built for, and only once. No fallback sets `Harness=nil`.

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
Still H5: the public get/propose tool(s) and their grants, the editor and controls, the auto-narrowing and executable
policy settings, the exact structured executable confirmation view, and mapping `PublicationError` for a client.
