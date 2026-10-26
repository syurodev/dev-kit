# Subscription-path harness tuning: measurement, workspace harness profile, smaller context

Date: 2026-10-26. Status: **design, not built**. Nothing here is implemented. Everything under "Measured" was read from the
person's database and the code on 2026-10-26.

## 1. Direction

DevKit is a layer around the vendors' own harnesses (Claude Code, Codex, opencode). The person mostly runs these on a
subscription, so the work that pays off daily is tuning what DevKit controls around them, not writing an agent loop. A native Go
engine on API keys (OpenRouter) stays a later, separate project; this spec only keeps its seams open by making the profile
engine-independent.

DevKit controls: the tools it serves over MCP (schemas and result sizes), the prompt it appends, skills and MCP servers it
configures, and which account a session runs as. It does not control the vendor's loop, compaction, or cache.

Goals: save tokens on the subscription path, tune per workspace, and let an agent propose changes to its own workspace's
profile with the person's approval.

Non-goals: a native engine, any change to vendor compaction, usage for Codex/opencode (§3.1), hot-editing the DevKit prompt
text, and any change to the permission model (grants stay per subject, gates stay where they are).

## 2. Measured

- **Usage rows.** 512 Claude turns (2026-09-17 .. 2026-10-26) in `agent_turns`. The structured fields of `usage_json` are zero
  for turns before ~2026-10-25 because `NormalizeUsage` did not read `quota.token_count`; the numbers are intact in
  `RawMeta.quota.token_count` (with a per-model `model_usage`). Read from raw: about 961M tokens, 97.1% cache reads,
  ~25M cache writes, ~3.1M output. Per turn: p50 1.1M, p90 4.7M, p99 9.3M, max 20.9M. The heaviest 10% of turns are 39% of
  tokens. By workspace: `fsap-security` 824M (86%), `personal-finance` 118M (68 turns, one session), the rest small.
  Whether the subscription limit weighs cache reads is not known.
- **opencode / Codex.** opencode has 44 turns and Codex 1 with no usable usage.
- **Tool schemas.** `tools/list` in the test configuration has 78 tools and 39,658 bytes (a lower bound: the fake deps leave out
  some groups; the source has ~150 `AddTool` sites). By group: codegraph 17,204, agenthub 6,720, workspace 5,121, notes 3,764,
  db 2,607, kafka 2,123, ssh 1,061. Order is stable (8 of 8 runs).
- **Result budget.** `ResultBudget` is 24 KB (~6k tokens) per result and cuts the head only (`internal/mcp/result_budget.go`).
- **Config today.** `AgentCustomization` (skills, MCP) is per vendor in `preferences`; nothing is per workspace.
  `workspaces` has an `instructions` column. `SessionHost` opens one identity-bound MCP socket per session.

## 3. Phase 1: measurement

1. **Read usage with a fallback.** One function reads a turn's usage from the structured fields, else from `RawMeta`. No
   migration; raw is preserved.
2. **Report.** A Go service aggregates by workspace, session, model and day: turns, fresh input, cache read, cache write,
   output. Exposed as a UI-only bridge method (`uiOnly`, like the account methods), not an MCP tool. Numbers only, never content.
3. **Tool result sizes.** Record bytes per tool call by tool (no content), so budgets in phase 3 follow data.
4. **UI.** A table under Settings → Agents; placement is decided in the plan.
5. **Scope.** Claude only. Other vendors show "no data".

Done when the report reproduces the §2 numbers and tests cover both usage shapes.

## 4. Phase 2: workspace harness profile

**Storage.** Table `workspace_harness_profiles` (workspace id, revision, JSON), updated by compare-and-swap on `revision` like
`AgentCustomization`. No secrets (env var names only). `workspaces.instructions` stays where it is. No profile means today's behavior.

**Fields (first release).**
1. `toolGroups`: on/off per DevKit tool group. A group is the part of a tool name before the first dot (`codegraph`, `git`, `pr`,
   `tasks`, `db`, `kafka`, `ssh`, `pdf`, `notes`, `agenthub`, ...). The capability registry design is not built and is not a dependency.
2. `resultBudget`: per tool or group size and cut mode (`head` default, `headTail`).
3. An overlay on `AgentCustomization` per vendor (disabled skills, disabled MCP, servers, skill folders). Workspace wins over vendor.

**Applying.** The profile is resolved when `OpenSpec` is built (where `Customizations` is merged today). The session's MCP
socket filters `tools/list` by `toolGroups` and applies the result budget. A tool of an off group is also refused when called
directly. Saved changes apply to new or reloaded sessions.

**Mid-session grant.** An always-present tool, `harness.profile.propose`, lists the groups that are off, one line each with the
estimated schema cost, so an agent knows what it can ask for. A request to add group B asks the person and offers two scopes:
**this session only** (ends with the session, profile unchanged) or **save to the workspace**. If the vendor refreshes tools on
`notifications/tools/list_changed`, B is usable on the next model call, at the cost of rewriting the cache once. Otherwise the
request is recorded and the UI says "Reload to apply". Enabling B does not approve calls to B's tools; grants and gates run per call.

**Agent edits.** Tools `harness.profile.get` (read) and `harness.profile.propose` (write), scoped to the caller's own workspace.
Flow: the agent sends a change; Go computes the diff against the current revision; the person sees a confirmation of that diff
(the agent's rationale appears only as labelled untrusted text); approval applies it by compare-and-swap, and a moved revision is
a conflict. No allow-always. Server mode and a missing prompt handler fail closed. Every change is audited without content.

| Change | Default |
| --- | --- |
| Narrowing (hide a group, lower a budget, disable a skill or MCP server) | Asks. A UI-only setting lets the agent apply narrowing without asking; off by default |
| Widening that runs no code (re-enable a group, raise a budget) | Always asks |
| Fields that run code or name paths (`servers` command, `skillFolders`) | The person chose to allow proposals. The confirmation shows the exact command, arguments, resolved paths and env var names, built by Go; the approve button is not focused; one such change per proposal; effective in new sessions only |

**UI-only for good.** Accounts, sign-in and the vault (`TestNoToolTouchesAgentAccounts` stays), sandbox and permission
settings, the approval policy itself, and the auto-narrowing setting. An agent that could change these could remove its own limits.

**Errors.** New public codes (names are proposals): `harness.profileConflict`, `harness.profileDenied`, `harness.groupUnknown`.
Each is published in `bridge.keptCodes`.

## 5. Phase 3: smaller context

- The profile editor shows the schema cost of each group (measured from `tools/list` by Go).
- Trim the `codegraph` schemas without changing behavior, and add a total-size ceiling test for `tools/list`, as the existing ratchets do.
- `headTail` cut in `result_budget.go`, used only for tools the profile names (for example `ssh.runCommand`). Default stays head-only.
- Prompt sections that mention tools of an off group are left out, so the prompt does not describe tools that are not served.
  `TestPromptNamesOnlyRealTools` keeps its meaning.

## 6. Phase 4: cache stability

- Tests: `tools/list` is deterministic; the DevKit prompt is byte-identical for the same inputs during a session; saved profile
  changes apply only at open or reload.
- Measure the cost of an account move with the phase 1 report (cache write right after a move) before touching `accountFlipHold`
  (5 minutes). No failover behavior changes in this spec without data.

## 7. Spikes to run first (unverified)

1. Does each vendor refresh its tool list on `tools/list_changed`, and what does it cost in cache writes? Decides the mid-session path.
2. Do MCP servers added by a profile run inside the vendor session's jail?
3. Does the DevKit prompt template divide into sections that can be tied to a tool group?
4. Can the existing `interaction.Hub` show a structured diff, or does it need a new view?
5. How `--allowedTools` and the pre-approved list behave when a group is hidden.
6. Why opencode records no usage.

## 8. Testing

Unit tests for usage reading (both shapes), the diff and compare-and-swap, tiering, group filtering, direct-call refusal, and
budget modes. Grants tests for the new tools (`TestNewContainerGrants*` style). `go test ./internal/arch/` for the new package.
Tool-list size ceiling test. Frontend: `bun test --isolate` and `bun run build` for the new panels. Bindings regenerated after any
`AppService` change.

## 9. Out of scope, later

The native Go engine on OpenRouter, a profile suggestion engine from measurements, Codex and opencode usage, and Harness²-style lessons.
