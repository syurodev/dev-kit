# Stale session-override repair (R1a-1)

Date: 2026-10-28. Production Harness stays inert. This is the PR description for `fix/stale-session-override-group`.

## Summary

F-C (#377) repairs a stale group or tool name in the stored **workspace** profile (W). The same staleness in a **session override** (S) was not covered: a session whose S names a group the build no longer registers could not resume (`harness.snapshotUnavailable`, "could not be resolved"), and the window's revoke failed with `harness.groupUnknown`. Closing the session cleared S and let it resume, so the person was stuck without knowing why, and the way out discarded their choices. Review finding R1a-1 (`docs/reviews/2026-10-28-r1a-fc-o2-dirswap-review.md`, with the repro `stale_session_override_group.go.txt`).

This change lets the window remove exactly those names from S, confirmed, without closing the session, and makes the resume refusal say what is wrong and what to do. It never widens, does not make the resolver permissive, and does not touch O-2 or the directory-swap work.

## Design

**Chosen: the window's existing Revoke accepts a name the registry lacks and routes it to a session-level repair that follows F-C.** The alternative, letting the ordinary revoke evaluate such a name, would need a before-state that does not resolve (S names an unknown group), so it would need a permissive branch in the evaluation; a separate new AppService method would add a binding, a contract-list entry and a frontend entry point. Routing reuses F-C's repair record and apply path and changes no bridge method, binding or published code.

- **Entry.** `PrepareRevoke` (window only) validates the names. A name the registry has takes the ordinary path, unchanged (and still fails on a stale S, which is the correct refusal). A name it lacks goes to `Service.PrepareSessionRepair`, which also requires the UI caller in Go. A request that mixes a registered and an unavailable name is refused.
- **Exact server-derived removals.** The names the window sends only select the repair. Each must be one of the groups that the stored S holds and the registry lacks, otherwise the fixed `groupUnknown` sentence. The removal is **every** such group, derived in Go from the stored S and the registry, and the review lists each one. S is never written with anything it did not already contain: the repaired S is a subset of the stored one, or deleted when empty, as an ordinary revoke leaves it.
- **No permissive resolver.** `harness.Resolve` and the launch snapshot are untouched and still refuse an unknown group. The repair checks that the repaired S resolves with the stored W for the session's vendor under the same inputs as a launch, and offers nothing otherwise. If W itself does not resolve, the refusal says to repair the workspace first (F-C), reusing `harness.profileInvalid`.
- **Always asks.** It goes through the same proposal service and shared hub confirmation (`EnvBlocks`: no allow-always, and auto-narrowing is never consulted). The review is labelled `narrowing` and says "removal only, nothing is turned on". It is not described as widening because nothing becomes available: the groups removed are ones the build does not serve.
- **Binding, CAS, spent on use.** The prepared repair binds the session, the exact S stamp and W stamp, the groups to remove, the registry identity, the session vendor's customization revision, the policy revision and the repaired S. Apply re-reads under the V boundary and the session's coordinator, requires the same binding, and writes S in one compare-and-swap transaction (one revision step). The id is single use and caller bound; cancel, replay and a stolen id change nothing.
- **Ambiguous outcome.** An unknown store result is read back (`Expect+1` committed, `Expect` proven not written, anything else uncertain) and reported as uncertain with its own wording; there is no automatic retry and no reconcile is offered.
- **Running sessions are not touched.** A session with an active, staged or paused enforcing endpoint is refused with `harness.notAvailable` (S moving under a served policy is the ordinary publication path). A running session cannot hold a group the registry lacks, so this is a fail-closed guard.
- **The refusal.** `Provider.Snapshot` returns `harness.ErrStaleSessionOverride` (code `harness.snapshotUnavailable`, already published, new fixed sentence) when the resolve fails, S names unregistered groups and W has no stale reference. Agent Hub passes it through instead of the generic sentence. It names the way out (Revoke the unavailable group in the session's Tool groups panel and confirm, then open the session again; closing also discards the choices) and no stored name. When W is also stale, or for any other cause, the generic sentence stays.
- **Window.** The session Tool groups card marks a group the build lacks as "not available in this build" and explains what Revoke does there. No new button, bridge method or binding.

## Never widens

Removal only: S is a set of groups to turn on, and the removed groups are not registered, so nothing is served that was not (a session with this S could not start at all). The applied reference, the runtime and the executable guards are not touched; W, other sessions, running endpoints, accounts and usage history are unchanged. A group that returns in a later build is not on for this session unless it is turned on again, which is a narrowing at worst. Agent, terminal and unknown callers are refused in Go at the facade, the service and Apply.

## Test plan

Composed tests in `internal/app/harness_stale_session_repair_test.go` run the real stack (SQL store, preferences and boundary, shared hub, Agent Hub and MCP server; only vendors and answers scripted):

- the report's repro as an end-to-end test: refusal text, denial changes nothing, approval removes S in one revision, the same session resumes with no Close and serves only what is registered and on; applied reference, W unchanged;
- only the unavailable group leaves S; a registered group is not removable through the path; mixed, unknown and unheld names are `groupUnknown`; an ordinary revoke on a healthy session is unchanged;
- agent (own and another session), terminal and unknown callers refused through the facade and the service, the prepared id unusable by them, no request can carry the repair target;
- replay, cancel, and eight changes while the question is open (S rewritten, gains a group, gains another stale group, W edited, W stale, vendor edit, policy off/on, session closed) all leave S as the newer state;
- always asks (auto-narrowing on, allow-always), headless refuses; W stale as well is repaired first; a running endpoint is refused and untouched; an ambiguous commit is reported as uncertain and not retried.

Plus `internal/harnessconfig/repair_session_test.go` (pure helpers, review text, refusal text), the bridge published-sentence tests, and `workspaceHarnessModel.test.ts`. Mutation checks: removing the binding comparison or the running-endpoint guard fails the tests above.

Evidence on `origin/main` (`8c5e64d0`) with the report's probe: `STALE-S LOCKOUT: resume=harness.snapshotUnavailable ... revoke=harness.groupUnknown ... snapshot=harness.snapshotUnavailable`.

## Limits

- The native window is not available here: the card change is covered by a model unit test and a typecheck/build, not run in the real window (AP3).
- A session whose S and W are both stale shows the generic refusal; repair W first (F-C), then the specific one appears.
- The removal covers every stale group at once; the review lists them all. More than 64 are refused, as in F-C.
- O-2, AP3/AP4, the directory-swap limit and F-D are unchanged. No activation, account or real workspace was used.
