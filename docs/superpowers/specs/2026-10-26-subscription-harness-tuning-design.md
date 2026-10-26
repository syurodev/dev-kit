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

1. **Reading usage.** The stored structured fields are not trusted. `NormalizedUsage.InputTokens`, `OutputTokens` and `CostUSD`
   are plain numbers that `json.Marshal` always writes, so a row stored while the normalizer did not understand the meta carries
   0 beside real numbers in `RawMeta` (a real row: 0 input and 0 output next to `inputTokens: 8` and `outputTokens: 6781`). A
   present field therefore says nothing about provenance. The report re-derives usage from `RawMeta` with the current normalizer
   of the turn's vendor, and uses the stored structured values only when `RawMeta` is empty. After that, a field is **known** only
   if the raw meta had it (a real 0 is known); otherwise it is **unknown**. Unknown is not 0: reports count turns without data
   separately and never add them as 0. The turn total is `totalTokens`. `model_usage` is the per-model split of the same totals,
   so it is used only for the by-model view and never added to `token_count`; a split that does not match its total is shown as
   partial. No migration; raw is preserved. New rows may also record a `usageVersion`, but the report does not depend on it.
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

Done when the report reproduces the §2 numbers and tests cover: a row whose stored fields are a false 0 next to real numbers in
`RawMeta`; a real 0 in the raw meta; a field absent from the raw meta; and a row with no `RawMeta`.

## 4. Phase 2: workspace harness profile

**Storage.** Table `workspace_harness_profiles` (workspace id, revision, JSON), updated by compare-and-swap on `revision` like
`AgentCustomization`. No secrets (env var names only). `workspaces.instructions` stays where it is. No profile means today's behavior.

**Effective configuration.** Inputs: the vendor's `AgentCustomization` (V), the workspace profile (W) and the session override (S, below).
An absent field in W inherits; a present field, even an empty list, is taken as written.

| Field | Rule |
| --- | --- |
| `toolGroups` (group → on/off; a group is the part of a tool name before the first dot) | Absent group is on. S can only turn groups on. |
| `resultBudget` (by tool or group, size and cut mode) | Precedence tool, then group, then the 24 KB default. Size 2 KB to 64 KB. `budgetExempt` tools cannot be overridden (validation error). `headTail` applies only to the plain-text cut; structured results keep `shrinkValue` and drop `nextCursor` as today. |
| `disabledSkills`, `disabledMCP` | Effective list is V ∪ W. W cannot remove an entry of V. |
| `servers` | Keyed by name. A W server replaces the V server of the same name. A duplicate name inside W is rejected. A name that collides with a server in the vendor's environment report is rejected, because which one wins is the vendor's rule. |
| `skillFolders` | V ∪ W, de-duplicated by resolved absolute path. |

**Classifying a change.** It is computed on the effective configuration before and after, never on the patch. Removing a W deny
entry can make a vendor-defined server or skill active again, so it is widening even though the patch only deletes a line.
- *Executable*: the effective set of server definitions or skill folders gains or alters one (command, arguments, URL, env var names, path).
- *Widening*: any other increase of capability (a group turned on, a larger effective budget for any tool, an effective deny entry gone).
- *Narrowing*: only decreases (a group off, a smaller effective budget for every tool that changed, a larger deny set, fewer servers or folders).
A patch with several effects takes the strictest tier: executable, then widening, then narrowing.
Budgets are compared per tool on the effective value (the tool's entry, else its group's, else 24 KB), not against the default:
2 KB to 16 KB is widening, 64 KB to 32 KB is narrowing, and deleting an override or changing a group's budget counts by the
tools whose effective value it changes. A change of cut mode alone is neutral and takes the narrowing tier.

**Applying: what is fixed at open and what is live.** The effective configuration is first resolved when `OpenSpec` is built
(where `Customizations` is merged today).

| Part | When it can change |
| --- | --- |
| Skills, MCP servers, skill folders, the appended prompt, the vendor's pre-approved tool list (`--allowedTools`, `PreApprovedMCP`) | Only at open or reload. They reach the vendor as launch arguments or session metadata. |
| Which tool groups the session's MCP server serves, and the result budgets | Live. The server evaluates the effective groups and budgets on every call, so a refusal or a new budget takes effect at once. |
| The vendor's view of the tool list | Only if the vendor refreshes on `tools/list_changed`; otherwise at reload. |

A call to a tool of an off group is refused by the server whatever the vendor's list shows (`harness.groupOff`). Saved changes to
W reach other running sessions only at their next open or reload; they keep the snapshot they opened with, and the UI marks them
"profile changed, reload to apply".

**Session override (S).** Table `session_harness_overrides` (session id, groups turned on, when). Version 1 holds group names only.
- It is a separate record from the saved profile and is never copied into it unless the person chose "save to workspace".
- Reload, resume and app restart keep it. This is what makes the "Reload to apply" fallback work. It is a statement about which
  tools are served, not an approval of any call to them, so it is not invalidated by vault lock; grants and gates still run per call.
- Closing or deleting the session removes it.
- A rewind fork keeps the override of the session it continues (how rewind assigns session rows is checked in the plan). A child
  session never inherits it: a child starts from the saved profile, so a parent cannot hand a child more than the person approved.
- The person can revoke it at any time; the session's summary card lists active overrides. Revoking takes effect at the server
  at once: later calls to that group are refused (`harness.groupOff`) even if the vendor's list still shows the tools, and the
  server sends `tools/list_changed` for vendors that honor it. The UI shows three states: active, revoked with the vendor's list
  waiting for a reload, and gone after the reload or close.

**Asking for a group mid-session.** An always-present tool, `harness.profile.propose`, lists the groups that are off, one line each
with its estimated definition cost, so an agent knows what it can ask for. The person chooses **this session only** (writes S) or
**save to the workspace** (writes W **and** S for the requesting session, so that session has the group now as in the first case;
other sessions follow the table above). The server serves the group from the next call. If the vendor refreshes on
`notifications/tools/list_changed`, the tools appear in its list at the cost of rewriting the cache once; otherwise S is stored and
the UI says "Reload to apply". The prompt and the pre-approved list are fixed at open, so the result of the grant returns a short
paragraph generated from the same guidance section as the prompt, and says it replaces what the prompt said about that group being
off. A tool that would be pre-approved at open asks through the normal permission flow until the next reload.

**Agent edits.** Tools `harness.profile.get` (read) and `harness.profile.propose` (write), scoped to the caller's own workspace.
Go computes the effective before and after and shows the person that diff; the agent's rationale appears only as labelled untrusted
text. The proposal is bound to a fingerprint of **all** inputs of the diff: the revisions of V, W and S, and a digest of the
vendor-defined parts the diff depends on (the servers and skills in the vendor's environment report, with their commands). On
approval Go resolves the effective before and after again, under the same lock as the write, and refuses with
`harness.proposalStale` unless both match what the person saw; a moved W revision alone is `harness.profileConflict`. This closes
the case where a proposal removes a server override to fall back to the vendor's server and the vendor's config changes command
while the dialog is open. The applied record stores the fingerprint, and a session that opens with a different effective set of
executables than the one approved shows a notice. The vendor's own files are outside DevKit's control, so the guarantee covers what
the person saw at approval, not later edits to those files. No allow-always. Server mode and a missing prompt
handler fail closed. Every change is audited without content.

| Tier | Default |
| --- | --- |
| Narrowing | Asks. A UI-only setting lets the agent apply narrowing without asking; off by default |
| Widening | Always asks |
| Executable | The person chose to allow proposals. The confirmation shows the exact command, arguments, resolved paths and env var names, built by Go; the approve button is not focused; one such change per proposal; effective in new sessions only |

**UI-only for good.** Accounts, sign-in and the vault (`TestNoToolTouchesAgentAccounts` stays), sandbox and permission settings,
the approval policy itself, and the auto-narrowing setting.

**Errors.** New public codes (names are proposals): `harness.profileConflict`, `harness.profileDenied`, `harness.groupUnknown`,
`harness.budgetInvalid`, `harness.proposalStale`, `harness.groupOff`. Each is published in `bridge.keptCodes`.

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
9. Whether the environment report gives enough of the vendor-defined servers and skills (name, command, arguments) to fingerprint
   them and show them in the confirmation.

## 8. Testing

Unit tests for usage reading (the four fixtures in phase 1, and a split that does not match its total), effective configuration
(absent vs empty, duplicate and colliding server names, removing a deny entry), classification on before/after including mixed
patches and per-tool budget changes (2 to 16 KB, 64 to 32 KB, a deleted override, a changed group budget), proposal binding (the
vendor-defined server changes command, V changes, or S changes between proposal and approval, each refused as stale), override
lifecycle (reload, resume, restart, fork, child, close, revoke while running, calls refused after revoke), save-to-workspace
reaching the requesting session but not other running ones, group filtering and direct-call refusal, budget precedence, range and
exemptions, and `headTail` against structured results and cursors. Prompt tests: with each group off the always-present
rules are unchanged and no tool of that group is named. Grants tests for the new tools (`TestNewContainerGrants*` style).
`go test ./internal/arch/` for any new package. Extended `TestToolDefinitionsStayWithinBudget`. Frontend: `bun test --isolate` and
`bun run build`. Bindings regenerated after any `AppService` change.

## 9. Out of scope, later

The native Go engine on OpenRouter, a profile suggestion engine from measurements, Codex and opencode usage, and Harness²-style lessons.
