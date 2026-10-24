# Scoped unattended pre-approvals

Task: D1 / OPT-0a45f5. Date: 2026-10-25. Status: **proposal for explicit review; no permission policy code changed**.

## Direction and approval boundary

The person chose unattended work outside environments whose tag blocks auto. An environment requiring confirmation, including a missing/unresolvable tag under fail-closed behavior, must continue asking every time.

The existing D1 task explicitly requires: “Security-sensitive: spec approved by the person before any code.” This document makes the proposed scope, expiration and non-environment gates concrete so that review is the last step before implementing policy changes. The request to implement team foundations does not silently select all the unresolved choices below.

## Current behavior

`internal/interaction/hub.go` owns confirmations, caller/tool/profile-scoped in-memory allow-always and headless fast failure. `internal/mcp/gate.go` binds confirmation to the exact selected profile and reuses it for execution. PR mutation tools have action-owned confirmation, some using `EnvBlocks: true` even without a real environment tag. The capability gateway still decides permission and audit. Server mode exposes unauthenticated loopback HTTP and deliberately fails all interactive requests; this design does not let that bridge issue or consume approvals.

## Proposed initial scope

Pre-approval is an explicit local UI action by the person. It is not an agent-authored policy, a reusable “yes” from peer messages, or a side effect of enabling a work group.

| Action class | Proposed first release |
| --- | --- |
| Exact-profile DB/SSH/Kafka/API action on a resolved non-blocking env | Eligible only for a specifically listed operation and target; destructive/writes require an exact action/argument fingerprint. |
| Git operation on a named repo/branch/remote | Eligible only where the action's own policy supports pre-approval; protected/default branches, task ownership and push rules still apply. |
| Environment requires confirmation or cannot be resolved | Always ask; no pre-approval match is considered. Headless fails fast. |
| `pr.create`, `pr.update`, `pr.merge`, `pr.close`, `pr.deleteBranch` | Keep per-call confirmation in this first release. All currently have action-owned always-ask policy; allowing them unattended requires a separate explicit scope decision, with immutable-head binding for terminal actions. |
| `pr.comment`, `pr.reply` | Keep the existing per-call/allow-always policy; this first release adds no pre-approval path for PR mutations. |
| Cross-workspace peer messages | Keep per-message confirmation. No broad pre-approval that makes another workspace discoverable. |
| Model/effort choices above caller, account rotation policy | Use existing user-selected model profiles/account policy. No new unattended override. |
| Task acceptance | Role/revision and verify gates remain; a pre-approval cannot turn a failed verify into success. Boards without verify keep confirmation in this first release. |
| Account selection, profile selection, sign-in, secret reveal, settings/board administration | Not pre-approved. Selection must already be explicit or bound; no hidden choosing between identities. |

These defaults narrow the first release to reviewed, target-bound operations. The UI must enumerate eligible operations; “everything in dev” is not a supported scope. Operation eligibility is declared centrally with the capability registry, not inferred from tool-name substrings or an agent's readOnlyHint.

## Approval record and lifetime

Proposed record: opaque ID; schema/policy revision; user-created provenance; selected workspace/group or session principal; exact operation; resource/profile/repo/branch/remote identity; resolved env-policy revision; optional immutable revision/action-argument fingerprint; created/expires/revoked timestamps; maximum and consumed uses; issuer audit ID. No credential or full SQL/command body is logged as approval metadata.

Proposed defaults: one use, expires after 30 minutes; person may choose a shorter duration or an explicit finite use count, with an 8-hour hard cap for the first release. No wildcard operation, all-workspace, all-profile, unknown caller or unbounded-use record. These numerical defaults require approval; they are not an implementation commitment yet.

Store records locally with content protected according to the vault's policy. On restart, first-release approvals are invalidated rather than resurrected automatically. Locking the vault or pausing/revoking a group stops new approval-backed dispatch. An in-flight external effect cannot be undone by revocation; UI says which call already started.

## Evaluation order

1. Verify identity, current workspace/folder grants, task ownership and operation capability through the existing gateway.
2. Resolve the exact target once; select an identity/profile only through existing binding/selection rules. Capture the target/policy revision.
3. Evaluate current environment. Any confirmation-required, missing or ambiguous policy goes to the normal confirmation path; no pre-approval can override it.
4. Evaluate action-owned restrictions. Existing `EnvBlocks` is not simply ignored: separate environmental prohibition from action-owned always-ask policy explicitly before adding approval matching. Unknown classification fails closed.
5. Look for a user-issued matching, unexpired, unrevoked approval. Recheck scope, target identity and revisions; hash-match writes to the approved action. Atomically reserve one remaining use with a fencing token.
6. Immediately before the external operation, recheck relevant policy/target revision and revocation. If changed, refuse or ask; do not swap to the currently active profile. No transaction is held while waiting on a vendor/network.
7. Audit approval ID and decision, then execute against that exact target. Consume the reserved use once execution may have started, including ambiguous network failure; do not refund and retry a possibly completed write.

Normal tool authorization and action confirmation remain separate. Approvals cannot make a denied capability pass, grant a child broader paths, or enable a missing tool. A denial by the person is final for that request; the agent cannot obtain another approval by rewording it.

## Local UI and service boundaries

UI offers a preview of operation, target, workspace/principal, duration, use count and data/revision fingerprint before approval. Settings/group details show active approvals, last use and Revoke. UI-only methods create/revoke; agents may see only a scoped eligibility/status summary, never issue/extend an approval.

`interaction.Hub` consumes an injected approval evaluator. Policy matching lives in a leaf/domain service with locally declared interfaces; composition wires the store and current env resolver. Registry metadata declares eligibility and confirmation reason. Do not duplicate matching in DB, SSH, Kafka and Git handlers.

Server mode never accepts approval creation via its unauthenticated bridge. Supporting approved background work requires a trusted identity-bound desktop/session path; do not set `Headless=false` or install a fake prompter to make tests pass. If that trusted path is unavailable, refuse with a readable reason.

## Recovery, audit and UI semantics

- Expiration/revocation is checked at reservation and immediately before execution. Changing profile/env/remote bindings invalidates the match.
- Crashed reserved uses become consumed/unknown; human reconciliation is required for ambiguous writes.
- Approval decisions are auditable by opaque IDs, caller/operation/target and outcome; no tokens, SQL, command bodies or peer message text.
- “Needs you” only appears if a real confirmation/selection/decision is waiting; a failed pre-approval match does not auto-create a model turn.
- Work group pause stops dispatch, not already-running external effects. No auto-renewal or recurring model heartbeat is added.

## Required implementation tests after approval

- Env wins for prod/custom confirmation tags, removed tags, resolver errors and revision races.
- Same operation on another profile, branch, workspace, account or changed argument fingerprint cannot reuse approval.
- Race two calls for the last use; only one executes. Expiry/revoke between reservation and execution prevents use.
- Unknown action classification, missing policy service/store, locked vault and headless identity mismatch fail closed.
- No blanket bypass of PR, cross-workspace, model-cost, task-verify or branch-protection gates.
- Crash before/after external execution never automatically refunds/replays a possibly completed write.
- Gateway grants and audit are exercised with the real container; UI-only approval methods are absent from MCP.
- Manual confirmation, allow-always and explicit denial retain existing semantics when no pre-approval is configured.

## Decisions requested before code

Review the operation eligibility matrix, one-use/30-minute default, 8-hour cap, restart invalidation and exclusion of PR mutations/cross-workspace messages/task closure without verify. Any broader choice needs its own concrete target/revision matching rules before implementation.

Related: [Team orchestration design](2026-10-24-agent-hub-team-orchestration-design.md), D1 task description, capability registry A1–A4. This document changes no current grant, confirmation, account or environment policy.
