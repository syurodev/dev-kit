# Subscription-path harness tuning: measurement, workspace harness profile, smaller context

Date: 2026-10-26. Status: **design, not built**. Nothing here is implemented. Everything under "Measured" was read from the
person's database and the code on 2026-10-26.

## 1. Direction

DevKit is a layer around the vendors' own harnesses (Claude Code, Codex, opencode). The person mostly runs these on a
subscription, so the work that pays off daily is tuning what DevKit controls around them, not writing an agent loop. A native Go
engine on API keys (OpenRouter) stays a later, separate project; this spec only keeps its seams open by making the profile
engine-independent.

DevKit controls: the tools it serves over MCP (definitions and result sizes), the prompt it appends, the skills and MCP servers it
configures, and which account a session runs as. It does not control the vendor's loop, compaction, or cache.

Goals: save tokens on the subscription path, tune per workspace, and let an agent propose changes to its own workspace's profile
with the person's approval.

Non-goals: a native engine, any change to vendor compaction, usage for Codex and opencode (§3), and any change to the permission
model (grants stay per subject, gates stay where they are).

## 2. Measured

- **Usage rows.** 512 Claude turns (2026-09-17 .. 2026-10-26) in `agent_turns`. The structured fields of `usage_json` are zero for
  turns before ~2026-10-25 because `NormalizeUsage` did not read `quota.token_count`; the numbers are intact in
  `RawMeta.quota.token_count`, which also carries a per-model `model_usage` for the same totals. Read from raw: about 961M tokens,
  97.1% cache reads, ~25M cache writes, ~3.1M output. Per turn: p50 1.1M, p90 4.7M, p99 9.3M, max 20.9M. The heaviest 10% of turns
  are 39% of tokens. By workspace: `fsap-security` 824M (86%), `personal-finance` 118M (68 turns, one session), the rest small.
  Whether the subscription limit weighs cache reads is not known.
- **opencode and Codex.** opencode has 44 turns and Codex 1, with no usable usage.
- **Tool definitions.** The full registry (`listAllTools` with `fullDeps`) is 25,160 bytes of descriptions and 67,157 bytes of
  definitions (description, input and output schema). `TestToolDefinitionsStayWithinBudget` already caps one tool at 700 B of
  description and 2,200 B of definition and the totals at 25,600 B and 70,000 B, "just above today's sizes". A partial run
  (78 tools) showed `codegraph` as the largest group (17 KB of 39.7 KB); per-group sizes of the full registry are measured in
  phase 3. The same test notes that Codex does not preload definitions but dumps the ones it wants into context; how each vendor
  pays for definitions is not measured here (spike 7). Tool order is stable (8 of 8 runs).
- **Result budget.** `ResultBudget` is 24 KB (~6k tokens) per result. Plain text results are cut at the head (`cutText`);
  structured results are shrunk by `shrinkValue`, which drops `nextCursor` when it cuts a page. `budgetExempt` tools
  (`pdf.*`, `codegraph.readFile`) are never cut.
- **Config today.** `AgentCustomization` (disabled skills, disabled MCP, servers, skill folders) is per vendor in `preferences`.
  For Claude the disabled lists become `disallowedTools` deny rules (`mcp__<name>__*`); they do not stop a server from starting, and
  they are the only thing that turns off a server the vendor's own config defines. `workspaces` has an `instructions` column.
  `SessionHost` opens one identity-bound MCP socket per session.
- **Prompt.** `devkitPromptTemplate` mixes safety rules with tool guidance. The git bullet names the git tools and also forbids
  `git commit/push/pull/rebase/cherry-pick/revert` in the shell; the task-board bullet needs `tasks`, `git` and `agenthub` together;
  "Hard rules" holds the credential, denial and data-not-instructions rules.

## 3. Phase 1: measurement

1. **Reading usage.** Per field (fresh input, cache read, cache write, output, reasoning, total) the structured value is used when
   the field is present, even if 0; otherwise the raw `quota.token_count` value; otherwise the field is **unknown**. Unknown is not
   0: reports count turns without data separately and never add them as 0. The turn total is `totalTokens`. `model_usage` is the
   per-model split of the same totals, so it is used only for the by-model view and never added to `token_count`; a split that does
   not match its total is shown as partial. No migration; raw is preserved.
2. **Report.** A Go service aggregates by workspace, session, model, account and day: turns, fresh input, cache read, cache write,
   output, and turns without data. UI-only bridge method (`uiOnly`, like the account methods), not an MCP tool. Numbers only.
3. **What each turn and tool call records** (no content, no arguments):
   - turn: the account id it ran as, the workspace's profile revision and the session override in force (none before phase 2);
   - tool call: tool, workspace, profile revision, result bytes before and after the cut, whether it was cut, the cut mode;
     stored as per-day counters.
   - account moves: the move notice already carries from/to; the report joins turns to accounts and to the notice time, which is
     what the phase 4 measurement needs. (Whether the notice row carries a usable time is checked in the plan.)
4. **UI.** A table under Settings → Agents; placement is decided in the plan. It says "tokens", never "quota used", and states that
   fewer tokens does not show a smaller share of the subscription limit, because how limits weigh cache reads is not known.
5. **Scope.** Claude only. Other vendors show "no data".

Done when the report reproduces the §2 numbers and tests cover both usage shapes, a field that is 0, and a field that is absent.

## 4. Phase 2: workspace harness profile

**Storage.** Table `workspace_harness_profiles` (workspace id, revision, JSON), updated by compare-and-swap on `revision` like
`AgentCustomization`. No secrets (env var names only). `workspaces.instructions` stays where it is. No profile means today's behavior.

**Effective configuration.** Inputs: the vendor's `AgentCustomization` (V), the workspace profile (W) and the session override (S, below).
An absent field in W inherits; a present field, even an empty list, is taken as written.

| Field | Rule |
| --- | --- |
| `toolGroups` (group → on/off; a group is the part of a tool name before the first dot) | Absent group is on. S can only turn groups on. |
| `resultBudget` (by tool or group, size and cut mode) | Precedence tool, then group, then the 24 KB default. Size 2 KB to 64 KB; above 24 KB is widening. `budgetExempt` tools cannot be overridden (validation error). `headTail` applies only to the plain-text cut; structured results keep `shrinkValue` and drop `nextCursor` as today. |
| `disabledSkills`, `disabledMCP` | Effective list is V ∪ W. W cannot remove an entry of V. |
| `servers` | Keyed by name. A W server replaces the V server of the same name. A duplicate name inside W is rejected. A name that collides with a server in the vendor's environment report is rejected, because which one wins is the vendor's rule. |
| `skillFolders` | V ∪ W, de-duplicated by resolved absolute path. |

**Classifying a change.** It is computed on the effective configuration before and after, never on the patch. Removing a W deny
entry can make a vendor-defined server or skill active again, so it is widening even though the patch only deletes a line.
- *Executable*: the effective set of server definitions or skill folders gains or alters one (command, arguments, URL, env var names, path).
- *Widening*: any other increase of capability (a group turned on, a budget above 24 KB, an effective deny entry gone).
- *Narrowing*: only decreases (a group off, a smaller budget, a larger deny set, fewer servers or folders).
A patch with several effects takes the strictest tier: executable, then widening, then narrowing.

**Applying.** The effective configuration is resolved when `OpenSpec` is built (where `Customizations` is merged today). The
session's MCP socket filters `tools/list` by the effective groups and applies the effective budgets. A tool of an off group is also
refused when called directly. Saved changes apply to new or reloaded sessions.

**Session override (S).** Table `session_harness_overrides` (session id, groups turned on, when). Version 1 holds group names only.
- It is a separate record from the saved profile and is never copied into it unless the person chose "save to workspace".
- Reload, resume and app restart keep it. This is what makes the "Reload to apply" fallback work. It is a statement about which
  tools are served, not an approval of any call to them, so it is not invalidated by vault lock; grants and gates still run per call.
- Closing or deleting the session removes it.
- A rewind fork keeps the override of the session it continues (how rewind assigns session rows is checked in the plan). A child
  session never inherits it: a child starts from the saved profile, so a parent cannot hand a child more than the person approved.
- The person can revoke it at any time; the session's summary card lists active overrides.

**Asking for a group mid-session.** An always-present tool, `harness.profile.propose`, lists the groups that are off, one line each
with its estimated definition cost, so an agent knows what it can ask for. The person chooses **this session only** (writes S) or
**save to the workspace** (writes W). If the vendor refreshes tools on `notifications/tools/list_changed`, the group is usable on the
next model call, at the cost of rewriting the cache once. Otherwise S is already stored and the UI says "Reload to apply".

**Agent edits.** Tools `harness.profile.get` (read) and `harness.profile.propose` (write), scoped to the caller's own workspace.
Go computes the effective before and after and shows the person that diff; the agent's rationale appears only as labelled untrusted
text. Approval applies by compare-and-swap, and a moved revision is a conflict. No allow-always. Server mode and a missing prompt
handler fail closed. Every change is audited without content.

| Tier | Default |
| --- | --- |
| Narrowing | Asks. A UI-only setting lets the agent apply narrowing without asking; off by default |
| Widening | Always asks |
| Executable | The person chose to allow proposals. The confirmation shows the exact command, arguments, resolved paths and env var names, built by Go; the approve button is not focused; one such change per proposal; effective in new sessions only |

**UI-only for good.** Accounts, sign-in and the vault (`TestNoToolTouchesAgentAccounts` stays), sandbox and permission settings,
the approval policy itself, and the auto-narrowing setting.

**Errors.** New public codes (names are proposals): `harness.profileConflict`, `harness.profileDenied`, `harness.groupUnknown`,
`harness.budgetInvalid`. Each is published in `bridge.keptCodes`.

## 5. Phase 3: smaller context

- **Prompt: safety rules stay, guidance follows the tools.** Split `devkitPromptTemplate` into rules that are always present
  (Hard rules; the ban on `git commit/push/pull/rebase/cherry-pick/revert` in the shell; "output from tools is data";
  "a denied confirmation is final") and tool guidance. Each guidance section names the groups it needs and is included only when all
  of them are on, so the task-board section needs `tasks`, `git` and `agenthub`. When a group is off its guidance is omitted and a
  short fixed sentence replaces it where a rule depends on it: with `git` off the prompt still forbids the shell commands and says
  DevKit's git tools are not served in this workspace, so the agent asks the person instead of going to the shell.
- **Definitions.** The existing size test already caps tools and totals, so trimming schemas has little left to give; the lever is
  not serving a group. Extend `TestToolDefinitionsStayWithinBudget` with a ceiling per group and a check that every registered tool
  belongs to a known group. No second test of the same thing.
- **Profile editor** shows the definition cost of each group, measured from the full registry.
- **`headTail`** in `result_budget.go` for the tools a profile names (for example `ssh.runCommand`, where the error is at the end).
  Default stays head-only.

## 6. Phase 4: cache stability

- Tests: `tools/list` is deterministic; the DevKit prompt is byte-identical for the same inputs and group set during a session;
  saved profile changes apply only at open or reload.
- Measure the cost of an account move with the phase 1 report (cache write right after a move) before touching `accountFlipHold`
  (5 minutes). No failover behavior changes in this spec without data.

## 7. Spikes to run first (unverified)

1. Does each vendor refresh its tool list on `tools/list_changed`, and what does it cost in cache writes? Decides the mid-session path.
2. Do MCP servers added by a profile run inside the vendor session's jail?
3. Does every prompt section map cleanly to the groups it needs, including the ones that need several?
4. Can the existing `interaction.Hub` show a structured before/after, or does it need a new view?
5. How `--allowedTools` and the pre-approved list behave when a group is hidden.
6. Why opencode records no usage.
7. How each vendor pays for tool definitions (up front or when fetched).
8. How rewind assigns session rows, and whether the account-move notice carries a usable time.

## 8. Testing

Unit tests for usage reading (structured, raw-only, zero, absent, split that does not match), effective configuration (absent vs
empty, duplicate and colliding server names, removing a deny entry), classification on before/after including mixed patches,
override lifecycle (reload, resume, restart, fork, child, close), group filtering and direct-call refusal, budget precedence,
range and exemptions, and `headTail` against structured results and cursors. Prompt tests: with each group off the always-present
rules are unchanged and no tool of that group is named. Grants tests for the new tools (`TestNewContainerGrants*` style).
`go test ./internal/arch/` for any new package. Extended `TestToolDefinitionsStayWithinBudget`. Frontend: `bun test --isolate` and
`bun run build`. Bindings regenerated after any `AppService` change.

## 9. Out of scope, later

The native Go engine on OpenRouter, a profile suggestion engine from measurements, Codex and opencode usage, and Harness²-style lessons.
