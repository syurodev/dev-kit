- **The frontend reads a vendor table** (`frontend/src/modules/agenthub/vendors.ts`:
  name, runtime id, how it signs in, colour). Do not compare against `"claude"`; an
  unknown vendor gets its own name and nothing to install or sign in to.
- **The window's pre-session reads need their own grants** (`catalog:*` for `AgentHubConfigCatalog`, `environment:*` for
  `AgentHubEnvironment` without a session). They were asked for since #30fedee3 / #231 with no grant covering them, so the
  gateway refused every call with a bare "Permission denied." — the draft picker's "Models unavailable" and the empty model
  list in Settings → Agents → "Models by job" (which then fell back to typing a model id). The service still answers only
  the window. `TestNewContainerGrantsAgentHubScopes` and `TestAgentHubPreSessionReadsAreNotRefusedByTheGateway` pin it;
  when a bridge method asks for a new scope, add its grant and a line to the former.
- **Both vendors pick model and effort before the session exists.** `VendorConfigCatalog`
  (`ConfigCatalog`) is implemented by Codex (`codex_config.go`) and Claude (`claude_config.go`):
  a short-lived agent in a scratch folder, no prompt. Claude only lists the effort levels of the
  model it is on, so the catalog switches to each model and reads them back from
  `session/set_config_option`. The draft picker calls effort `reasoning_effort` for every vendor;
  `acpRuntime.SetConfigOption` maps it to Claude's own `effort`.
- **Codex signs in inside DevKit, UI only.** Bridge `AgentHubSignInStatus` /
  `AgentHubStart{Browser,Device}SignIn` / `AgentHubCancelSignIn` / `AgentHubSignOut`
  (`auth:codex`), event `agenthub:signInCompleted`. There is no MCP tool for it
  (`TestNoToolSignsAgentsIn`), and `Service` refuses a caller stamped as an agent or a
  user-terminal MCP client. Audit lines carry the method and account id, never the
  login page, code or key.
- **Codex API key = an Accounts token, by id.** `preferences` stores only the id
  (`agent_codex_api_key_account`); `app.CodexAPIKey` resolves it at spawn and Accounts
  refuses to delete it while chosen. A successful ChatGPT sign-in adds a `KindExternal`
  "Codex (ChatGPT)" row to Accounts; signing out removes it.
- `agent_not_installed` / `agent_auth_required` name the vendor: adapters wrap those
  sentinels in `agenthub.VendorError`, and `bridge/errors.go` words the message per
  vendor. A chosen API key that is gone or refused also carries
  `ErrAgentAPIKeyUnusable` → `agent_api_key_unusable`, which the UI sends to Settings
  rather than to a sign-in (the key wins at every spawn). `app.CodexAPIKey.Choose`
  refuses agent / user-terminal callers like sign-in does.
- **A session nobody has prompted has no conversation to resume** (`Service.neverPrompted`, `resumeRuntime`).
  Codex writes none before the first turn (measured with the real app-server 0.163.0: `thread/resume` of such a
  thread answers "no rollout found for thread id"); Claude Code keeps its session file from the first message
  (not measured here — reopening is harmless either way, since nothing is lost). So a session with
  no turn row and no user message is reopened with `adapter.Open`, not `Load`, and its vendor id updated
  (`UpdateSessionACPID`); the model/effort chosen at creation come back from the stored `config_option_update`.
  Before this, every settings call on such a session after a restart failed, which the picker showed as
  "Models unavailable". Tests of the resume path must say the session was prompted (`markPrompted`).
- **A fresh Claude session carries its model list from the handshake.** claude-agent-acp sends the settings once,
  in the answer to `session/new` / `session/load`, and never a `config_option_update` until one changes — and
  DevKit used to drop that answer (only the catalog read it). `acpRuntime` now keeps it (`options`), folds
  every `set_config_option` answer into it, announces it to the first update handler (as `codexRuntime` does)
  and re-publishes it on `PrepareSession`. A refused model list reaches the picker as
  `agenthub.modelsUnavailable` ("Codex didn't report which models it offers…"), not "Agent process failed.".
- **A child session may write what its parent may write, never more** (`Service.CreateSession`, `inheritedExtraPaths`,
  `resolveCwd`, `checkChildFolderInWorkspace`). An agent-created child gets its parent's stored `ExtraReadWritePaths`
  (cleaned; read from the parent's row, never from the tool input — `agentHubCreateInput` has no path field,
  `TestCreateAgentSessionHasNoPathGrantInput`, and an agent caller's own `CreateInput.ExtraReadWritePaths` is still
  ignored) and persists them on its own row, so resume/rewind/reload rebuild the same jail, the brief and
  `workspace.list`/`git.*` `granted` (all read project path + stored extras) list them, and a grandchild inherits from
  its own parent: authority only narrows or stays equal along a chain. Its cwd may be inside the parent's project **or**
  inside one of the parent's extra folders (symlinks resolved on both sides; else `ErrCwdOutsideParent`), and a cwd in an
  extra folder must also be a folder of the parent's workspace when the workspace and a lookup exist. The child does not
  receive the parent's *project* folder as an extra (a child opened in an extra folder cannot write the parent's cwd; one
  opened in a subfolder stays in it). Before this a child had no extras, so a Codex child refused work in folders its
  parent could write. A person-created session is unchanged. Not run against the real vendors' sandboxes: the tests read
  the `OpenSpec` a fake adapter received.
- **An agent chooses a child's model and effort, and the person's profiles guide it**
  (`agenthub/model_catalog.go`, `model_choice.go`, `mcp/tools_agenthub.go`, `preferences/modelprofiles.go`,
  `bridge/agenthub_modelprofiles.go`, Settings → Agents → "Models by job" in `settings/ModelProfilesEditor.tsx`).
  - `agenthub.createAgentSession` takes `initialConfig {model, effort}` (effort is `reasoning_effort` inside)
    and `profile` (a profile's name or id). Nothing said → the child inherits its parent's model and effort
    when it is the same vendor (a value the runtime refuses is dropped). Values are checked against the
    vendor's catalog; a wrong one is refused with what the vendor offers. The parent-only, parent-folder and
    parent-workspace rules are untouched (`ErrCwdOutsideParent`, `ErrWorkspaceMismatch`).
  - **Cost policy** (`PlanModelChoice`): a profile is the person's own choice and asks nothing. By hand, a model
    other than the parent's asks the person — DevKit has no reliable price order for models, so cheaper asks
    too — and an effort above the parent's asks (efforts are ranked: none < minimal < low < medium < high <
    xhigh < max; "default" or an unknown name is unranked and asks). The question is the "DevKit tool gate"
    (`KindAgent`, `EnvBlocks: true`: every time, no allow-always). `CreateSession` repeats the check and refuses
    with `ErrModelChoiceNeedsApproval` unless `CreateInput.ModelChoiceApproved`, which only the tool handler
    sets after the answer (its input struct has no field for it).
  - `agenthub.listModels` / `agenthub.listModelProfiles` are read-only (`agenthub:read` on `models:list` /
    `profiles:list`; `mcp.AgentHubModelsGrant` / `AgentHubProfilesGrant`, checked against the real policy in
    `TestNewContainerGrantsTheModelReadTools`). The catalog is cached per vendor (`Service.ModelCatalog`: 15 min,
    a failure 1 min, one throw-away agent at a time, detached from the asking call); the window's own
    `ConfigCatalog` stays uncached and refreshes it. A vendor that cannot answer comes back with a status
    (`signInRequired`, `notInstalled`, `unavailable`) instead of failing the call.
  - **A long list is paged, a name is resolved** (`model_catalog.go` `pageModels`, `model_match.go`). `agenthub.listModels`
    takes `query` (words from the id or name, case/punctuation ignored), `limit` (default 40, max 100, per vendor) and
    `offset`; a vendor with hundreds of models (opencode) used to blow the 24 KB result budget and be cut blindly. A page
    carries `total`, `nextOffset` and a `hint`, names equal to the id and long descriptions are dropped. `PlanModelChoice`
    resolves what the agent wrote (`VendorModels.Resolve`: id, id in another case, display name, same letters/digits, a
    fragment naming exactly one model) to the vendor's id, and the cost comparison, the question to the person and the
    session use that id. A guess is never taken: an unknown or ambiguous name is a `ModelChoiceError` (an
    `ErrInvalidConfigOption`) whose sentence lists the whole list (≤ 12 models) or the closest few, and the bridge
    publishes it (`ModelChoiceDetail`) instead of "Choose a valid agent setting.". opencode lists efforts only for the model
    it is on, so an effort asked for another opencode model is refused as "takes no effort" — not fixed here.
  - **Profiles** are the preference `agent_model_profiles` (revision/CAS like `AgentCustomization`; ≤ 24, name 60,
    description 400, notes 800 bytes; unique names; ids are slugs of the name). Get/set/check are UI-only
    bridge methods (`AgentHubGetModelProfiles` / `SetModelProfiles` / `CheckModelProfiles`, grants
    `preferences:* agent_model_profiles`); no MCP tool edits them (`TestPromptTeachesModelChoiceAndNoToolEditsProfiles`).
    A profile whose model the vendor dropped is listed with `status` + `problem` and refused when chosen;
    its text reaches an agent as the person's configuration (`ModelProfilesNote`), never an instruction. The
    audit line of a child session names the profile, model and effort — never a prompt.
  - Not verified against the real vendors in CI: Claude/Codex model lists come from fakes
    (`TestFreshClaudeSession*`, `TestCodexSessionWithoutATurn*`, `choiceAdapter`); the Codex "no rollout" answer
    was measured with the real app-server 0.163.0 under a throw-away `CODEX_HOME`. Gallery:
    `?gallery#agenthub-model-profiles`.
- **A child session starts in its parent's permission mode** (`agenthub/mode_inherit.go`;
  `Service.CreateSession`, agent callers with a parent only). Read the live runtime's `Modes()` first, then the stored
  `current_mode_update` row when no live mode is available. Claude records successful `set_mode` changes through the
  update handler and deduplicates vendor echoes, including respecting a vendor's effective fallback mode.
  `agenthub.createAgentSession` has no mode input (`TestCreateAgentSessionHasNoModeInput`). Same vendor → parent's id.
  Claude → Codex/opencode: `plan` / `default` → `ask`; `acceptEdits` / `auto` → `auto`;
  `bypassPermissions` → `bypassPermissions`. `acceptEdits` → `auto` is a deliberate product choice because the other
  vendors have no edits-only mode. Codex/opencode → Claude: `ask` → `default`; `auto` → `auto` (if unavailable,
  `acceptEdits`, logged); `bypassPermissions` → itself. Claude ACP parses but does not advertise `dontAsk`; it is ranked
  conservatively with Ask and maps to the child's asking mode. Unknown ids remain unranked. An Ask / `default` / `plan`
  parent's child is closed with `ErrModeNotInherited` if a refused or missing mapping would leave it more permissive.
  Less strict parents allow a refused mapping to leave the vendor default, logged. A person-created session keeps the
  vendor default. Tests use fake vendors and ACP transports, not live vendor sessions.
- **A profile can also be a vendor subagent** (`internal/agenthub/subagents.go`, `claude_subagents.go`,
  `codex_subagents.go`; fields `subagent` + `prompt` on `AgentModelProfile`, off by default, so an old list changes nothing).
  A subagent is the vendor's feature (Claude's Agent tool, Codex's `spawn_agent`) running inside the session — not
  `agenthub.createAgentSession`. `applySubagents` runs beside `applySandboxExtras` at create, resume and rewind (never inside
  it: the catalog read calls that) and puts the vendor's own profiles in `OpenSpec.Subagents`; a running session keeps its set
  until Reload (Environment → Reload session). `planSubagents` leaves a profile out, with the reason, when its model/effort is
  not on the vendor's (cached) list, when Claude's effort is not `low|medium|high|xhigh|max`, or when the person already has a
  subagent of that name (`.claude/agents`, `.codex/agents`, `[agents.<n>]`; the programmatic definition would shadow theirs).
  `preferences.SubagentNames` derives the name from the profile **id** (ASCII slug, unique, never a vendor built-in).
  - Claude: `_meta.claudeCode.options.agents[<name>] = {description, prompt, model?, effort?}` and nothing else — no tools,
    permissionMode, mcpServers or skills (`TestClaudeSubagentsMetaWritesOnlyWhatCarriesNoPermission`). `model: "default"` is omitted.
  - Codex (**schema read from the 0.163.0 binary and run for real**): a role is `agents.<name>` = `{description, config_file}`
    (`AgentRoleToml`, + `nickname_candidates`); the role file needs `developer_instructions` and takes `model`,
    `model_reasoning_effort` (and `sandbox_mode`, which DevKit never writes). DevKit writes the file under
    `<codex home>/devkit-agents/<name>-<hash>.toml` (content-named so a running session keeps its file; jail may not write the
    dir; files unreferenced for 24 h are swept) and registers it per process with `-c agents.<name>.description=… -c
    agents.<name>.config_file=<abs>`, never in the shared config.toml. In the app-server (`multi_agent_v2`) the role appears in
    `collaboration.spawn_agent`'s `agent_type` with the description and "model … effort … cannot be changed". Codex only
    delegates when the person or an AGENTS.md asks for sub-agents — its own rule, stated in the UI.
  - `agenthub.listModelProfiles` also returns `subagent`, `subagentName`, `subagentStatus` (`active|skipped`) and
    `subagentProblem` (never `prompt`; only the window's own list carries it). No new tool or bridge method, so no new grant.
  - Real runtimes, mock model API, fake HOME: `cmd/pdfe2e/subagent_test.go` (skipped unless `DEVKIT_SUBAGENT_E2E_CLAUDE`,
    `_NODE`, `_CODEX` are set — see its header): a new Claude and a new Codex session each show the subagent, the child
    runs on the profile's model/effort/instructions, and a Claude subagent's shell `git commit` is refused by DevKit's hook.
    The harness gateway allows everything, so a gated tool's confirm card is not exercised through a subagent.
- **Codex subagent limits: one cap on how many, no depth** (`internal/preferences/subagentlimits.go`, preference
  `agent_subagent_limits`, `internal/agenthub/codex_subagent_limits.go`, bridge `AgentHubGet|SetSubagentLimits`
  (UI-only, revision/CAS, grants `preferences:* agent_subagent_limits`), Settings → Agents → Models & subagents →
  "Subagent limits", gallery `?gallery#agenthub-subagent-limits`). Unset = DevKit writes nothing. A value (0–16; Go
  rejects more, `codexSubagentLimitArgs` also clamps) is passed per process as `-c
  features.multi_agent_v2.max_concurrent_threads_per_session=N+1 -c agents.max_threads=N` beside the role registration
  (`applySubagentLimits` runs in `applySubagents`, so create, resume, rewind and Reload all re-read it; a running
  session keeps its limit until Reload). Never config.toml. No MCP tool reads or edits it
  (`TestNoToolTouchesSubagentLimits`). Claude: no control (its subagents cannot start subagents; DevKit sets no
  count), said in one line in the UI.
  **Measured with the real `codex app-server` 0.163.0** (`cmd/pdfe2e/subagent_limits_test.go`, scripted mock model,
  fake HOME; env as `subagent_test.go`; `go test ./cmd/pdfe2e/ -run SubagentLimit -v`):
  - **Slots**: the app-server runs multi_agent_v2 (`multi_agent_v2` is a stable, on feature; setting it false changes
    nothing). The root's developer message says "There are N available concurrency slots … including you". Default
    **N = 4** (the main agent + 3 subagents). `agents.max_threads=K` → K+1 slots (K subagents), and so does
    `agents.max_concurrent_threads_per_session=K`; `features.multi_agent_v2.max_concurrent_threads_per_session=M` → M
    slots (root included) and **wins over the agents.* keys when both are set** — hence DevKit sets both (the shared-home
    import carries the user's `features` table, and a `-c` table over a user's bool/table `multi_agent_v2` is
    accepted). Values up to 500 were accepted (no Codex cap; DevKit caps at 16). **`agents.max_threads=0` stops
    Codex starting** ("agents.max_concurrent_threads_per_session must be at least 1"), so "none" is expressed as the
    v2 key = 1 only (every spawn is refused). `agents.max_threads` is not the default 6 the docs suggest in this mode.
  - **A finished subagent frees its slot** (three children answered, then a fourth and fifth spawned fine); one that is
    still working keeps it. Nested spawns draw from the same pool: a chain root→a→b→c with 4 slots refused the c's child.
  - **A refused spawn** reaches the model as the `spawn_agent` call's output, the plain text `collab spawn failed:
    agent thread limit reached` — no error flag, the turn goes on.
  - **`agents.max_depth` does nothing here.** With room for 20 slots, depth 0, 1, 2 and the default all let a chain
    nest 4–7 levels (`/root/l0_w0/l1_w0/l2_w0/…`); `spawn_agent` stays offered to every level. ("Agent depth limit
    reached. Solve the task yourself." is in the binary, but this mode never produces it.) So nesting is bounded only by
    the slot count, and **DevKit offers no depth control** — a limit of 1 is the way to forbid nesting.
  - **`agents.job_max_runtime_seconds`** belongs to Codex's agent-job (CSV batch) tool, which is not among the tools the
    app-server offers (`exec wait … spawn_agent wait_agent`), so it is not exposed. `default_subagent_model` /
    `default_subagent_reasoning_effort` are covered by profiles-as-subagents.
  - Not verified: a real ChatGPT/API account may get different remote feature flags than the keyless runs above.
- **Codex sessions have session commands, DevKit's own** (`internal/agenthub/codex_commands.go`, `codexRuntime.Prompt`;
  `codexapp` `MethodThreadCompactStart` / `MethodReviewStart`). Claude announces its commands (`available_commands_update`)
  and runs them itself; `codex app-server` does neither — the TUI's slash commands are TUI features — so "/compact" used to
  reach the model as a sentence and the composer had no menu and no Compact button. Now the runtime announces `compact` and
  `review` once per process (same wire shape and session-state kind as Claude's, `SetHandlers` beside the model picker and
  connectors) and a prompt that is **exactly** `/name [text]` — one text block, nothing attached, name in `codexCommands` —
  runs through its RPC instead of `turn/start`. `see /compact`, `/etc/hosts`, an unknown `/x`, or a command with an
  attachment are ordinary prompts. Adding `/goal`, `/rename`, `/fork` later is one `codexCommands` entry + its `prepare`.
  - `/compact` → `thread/compact/start`; takes no text (the RPC has no instructions field: `/compact foo` is refused with
    `ErrInvalidPrompt`, not sent to the model). `/review [base <branch> | commit <sha> | instructions]` → `review/start`,
    `delivery: "inline"`; exactly two words `base X` / `commit <hex>` name a target, anything else is the person's own
    instructions (so "base on the style guide" is not a branch), a branch outside `[A-Za-z0-9._/@~^-]` is refused because
    Codex puts the name into a prompt whose model runs it in a shell. Nothing → `uncommittedChanges`.
  - **Busy is refused by DevKit, because Codex does not**: `thread/compact/start` and `review/start` on a thread with a
    running turn *interrupt that turn* and start theirs (measured). `beginTurnLocked` answers `ErrSessionBusy` first, shared
    with `Prompt`; the Compact button is hidden while the session runs (`compactOffered`, `slashCommands.ts`).
  - A command is a turn like any other: `r.turn` holds the thread, Stop → `turn/interrupt`, an app-server refusal of the
    request (bad thread, unreadable params) is `ErrTurnFailed` with Codex's sentence and an idle session, not
    `ErrProcessFailed` (`callRefused`). `thread/compact/start` answers `{}` without a turn id, so the turn is `adopt`ed from
    the next `turn/started` on the thread (`onTurnStarted`); no `turn/started` within `codexAdoptWait` (15 s) is an error,
    not a hang. After a compaction the translator's last token report is republished (`republishUsage`).
  - Transcript: `contextCompaction` → the **same card as Claude's** (`tool_call` name `compact`, kind `think`, title
    "Compact conversation"); `enteredReviewMode` → a `review` card ("Review: current changes"); `exitedReviewMode` → nothing
    (its text is the agent message that follows, measured identical). Before, all three drew the generic unknown-item card.
  - **Workspace context** (`Service.Invoke`): `slashCommandOf` (exactly one text block whose first word is `/name`, not a
    path) means the block is **not** prepended — it would turn "/compact" into a sentence for either vendor (Claude Code reads a
    command from the start of the turn, so this was very likely a latent bug there whenever the block was due: first turn,
    a change, every 20th turn — read from the code, not run against the real Claude) — and a successful `/compact` forgets what the session was sent (`forgetWorkspaceContextLocked`), so the next
    real prompt carries the block again. A failed compaction changes nothing. Not done: auto-compaction (either vendor)
    is not seen by `Service`, so the 20-turn refresh stays the only cover for it.
  - **Measured with the real `codex app-server` 0.163.0**, fake HOME, scripted mock Responses API (raw JSON-RPC probe; the
    DevKit path end to end is `cmd/pdfe2e/codex_commands_test.go`, run with `DEVKIT_SUBAGENT_E2E_CODEX=<binary>`):
    | | `thread/compact/start {threadId}` | `review/start {threadId, target, delivery}` |
    | --- | --- | --- |
    | answer | `{}` in ~50 ms, no turn id | `{turn, reviewThreadId}`; `reviewThreadId` = the thread itself for `inline` |
    | stream | `turn/started` (new turn) → `item/started contextCompaction` → `thread/tokenUsage/updated` → `item/completed` → `turn/completed completed` | `item/started+completed enteredReviewMode {review:"current changes"}` → `turn/started` of a *different inner turn* → userMessage (Codex's own review prompt) → `exitedReviewMode {review:<rendered findings>}` → agentMessage (same text) → `turn/completed` |
    | what ends it | `turn/completed` of the turn named by `turn/started` | `turn/completed` of the turn **in the answer**; the inner turn never completes and reads `interrupted` in `thread/read` |
    | model traffic | one `/responses` request ending in `compaction_trigger` (remote compaction v2); the reply must hold exactly one `compaction` output item or the turn fails ("expected exactly one compaction output item") | one request with a fresh context (developer + environment + Codex's review prompt), not the conversation; the result is appended to the thread afterwards |
    | tokens | `total` unchanged, `last.totalTokens` = ~7000 with 0 input (the new window's estimate; the context meter reads `last`) | an ordinary usage update |
    | busy thread | **interrupts the running turn** (it ends `interrupted`) and compacts | **interrupts the running turn** and reviews |
    | during it | `turn/start` fails `ActiveTurnNotSteerable { turn_kind: Compact }`; a second `thread/compact/start` interrupts the first | — |
    | fresh thread | works, no model request, `last` ≈ 7000 | works |
    | errors | `-32600 invalid thread id` for a bad id | same; `-32600` unknown target variant; `detached` delivery refused ("paginated threads do not support detached review") and deprecated |
    | notifications | **no `thread/compacted`** — the deprecated `ContextCompactedNotification` is not sent; the item is the signal | no `review/*` notification; raw model deltas are **not** streamed in review mode |
    `ReviewTarget`: `{type:"uncommittedChanges"}`, `{type:"baseBranch", branch}`, `{type:"commit", sha, title?}`, `{type:"custom",
    instructions}`. A branch or sha that does not exist is **not** validated by Codex (the review prompt just names it).
    Structured findings (`findings[]`, `overall_*`) render as "<explanation>\n\nReview comment:\n\n- [P1] title — path:lines\n  body".
    Not verified: a real model's review quality or token counts (mock only), 0.147 (the version named in older comments;
    only 0.162.1/0.163.0 were available), a ChatGPT account's remote feature flags.
  - **Deliberately not done**: `thread/fork`, `thread/revert`, `thread/goal/*`, `thread/name/set`, `thread/shellCommand`,
    `thread/inject_items` and `mcpServerStatus/list` have RPCs but no command — none was measured here and each needs its own
    decision (goal and name overlap Agent Hub's own goal/title; fork/revert overlap rewind). `delivery: "detached"` is
    refused by the app-server and deprecated. Codex's own `/model`, `/approvals` and the like are DevKit's pickers.
- **opencode is the third vendor, and the first with no pinned version** (`adapter_opencode.go`, `agentruntime.OpenCodeCLI`,
  `Agent.Unpinned`; measured against opencode V2 2.0.32 with a scripted model, not a real one). Research and plan:
  the conversation that added it; upstream state: `anomalyco/opencode` issues labelled `acp`.
  - **No pin.** `Install(ctx, "opencode", "")` asks npm for `@opencode/cli`'s latest tag (`Manager.resolveLatest`, stable
    versions only, `<NPMRegistry>/@opencode%2Fcli/latest`), installs it and activates it only if `OpenCodeAdapter.Probe` passes
    under the jail (initialize, and the agent can reopen a session). `Status.TestedVersion` is empty, so the UI says
    "Install latest" / "Update to X" and never "untested"; `Previous` is the rollback. DevKit knows what the agent can do from
    what it says (initialize, config options, `-32601` answers), never from a version number — Claude and Codex are meant to
    move to the same rule. `adapter_opencode_live_test.go` is the conformance check to run against a new release
    (`DEVKIT_OPENCODE_COMMAND=<native binary> go test ./internal/agenthub/ -run OpenCodeLive -v`; the binary is
    `node_modules/@opencode/cli-<platform>/bin/opencode`; `DEVKIT_OPENCODE_INSTALL_LIVE=1 … -run OpenCodeInstallLive` installs from npm).
  - **`opencode acp`, one process per session.** opencode registers a session's MCP servers per *directory* on its private
    server, so sessions sharing a process (or a directory) see each other's tools (upstream #40978). The process serves a private
    HTTP API on loopback (`AllowLoopbackListeners`) and ends when stdin closes. It reuses the generic ACP runtime
    (`acp_runtime.go`, `acpRuntime`) and wraps it in `openCodeRuntime`; what it needs that Claude does not is a hook
    (`acpRuntimeHooks`: `Type`, `PermissionBefore`, `PermissionAfter`), not a second runtime.
  - **Its home is DevKit's, like Codex's**: `<data>/agent-homes/opencode`, handed over as `XDG_{CONFIG,DATA,STATE,CACHE}_HOME`
    (opencode has no variable of its own). The user's `~/.config/opencode` and sign-in are never read. `config/opencode/` is not
    reset (a person may add providers there) but is read-only to the jail, as is `prompts/`. DevKit's settings ride on
    `OPENCODE_CONFIG_CONTENT`, which measurably beats every file: `permissions` (shell and edit **ask**), `update: disable`,
    `share: disabled`, and `instructions` (the DevKit prompt, a content-named file in `prompts/`). A private-home session gets
    `HOME=<home>/home`: opencode resolves `~/.claude` and `~/.agents` when it opens a session and, where the jail hides a real
    `~/.claude`, `realPath` fails and every `session/new` with it. `OPENCODE_DISABLE_CLAUDE_CODE=1`. XDG_CONFIG_HOME moves gh's and
    glab's login, so a shared home sets `GH_CONFIG_DIR`/`GLAB_CONFIG_DIR` back. Sign-in is `opencode acp --login` in a terminal
    tab (`AuthProvider`, the Claude path), run with the same XDG roots.
  - **Permission modes are DevKit's** (`ask`, `auto`, `bypassPermissions`, the ids Codex uses so a child inherits across vendors).
    opencode's own `mode` is an agent (build/plan) and `plan` does **not** stop writes (measured), so that option is dropped from
    the settings and `SetMode` refuses it. opencode is told to ask and `permissionBeforeAsk` answers: Full auto allows everything,
    Auto allows edits and puts commands to the person, Ask puts everything to the person. The person is never offered "always":
    opencode keeps that rule for the whole project, past the session (a later session ran the command unasked), so the option is
    removed from the card and an `always` answer becomes `once`. A refusal ends the turn (`stopReason: cancelled`). MCP tools are not in the
    ask list: DevKit's own gate asks after dispatch.
  - **What opencode does not have, and what DevKit does about it** (each is an interface the runtime/adapter does not implement
    or an error it answers, so nothing errors): steering mid-turn (`Steer` → `ErrUnsupported` → peer messages queue; the PR is
    upstream #54394) and goals; forking at a message (no `RuntimeForker`: a rewind restores files only); subagent profiles
    (`subagentProblem`: skipped with a reason); skills/MCP overrides and the environment report ("unavailable"); compaction cards
    (`session.compaction` is not declared, so opencode sends a plain marker). Unknown methods answer `-32601`.
  - **DevKit's tools are called through opencode's Code Mode**: its system prompt lists MCP tools as
    `tools.devkit.git_status()` (dots → underscores) and they only run inside the `execute` tool; past ~20 tools the catalog is
    cut to ~12 KB and the rest is found with `search` (`opencodeNaming`). It registers a session's MCP servers asynchronously: a
    prompt sent right after `session/new` was built without them (5 of 5), 100 ms after the server answered `tools/list` it had them
    (upstream #53299). `openCodeMCPSettle` (1.5 s) holds the first prompt; waiting for the MCP host to see `tools/list` would be exact.
  - **The jail must let opencode read the directories above the project** (`sandbox.Policy.ReadAncestors`, Seatbelt only): to open
    a session it resolves every ancestor of the project (and `<ancestor>/.claude`, `.agents`, `.opencode`, `opencode.json…`), and a
    stat-only grant on the ancestors made `session/new` fail with `EPERM: lstat '/Users'` — **for every project under /Users**, which
    no test caught because they all used a temp dir under /var (`TestOpenCodeLiveProjectUnderHome` is the one that does). Measured by
    editing the generated profile: `file-read-data` on the `/Users` literal is what it needs (`file-read-metadata`/`-xattr` are not
    enough), so ancestors are listable (names, never contents) for opencode only. A **private home** inside the home folder cannot
    be fixed that way — stat-ing the hidden `~/.claude` needs read access to that directory too, i.e. listing the home — so
    `openCodeNeedsSharedHome` refuses it up front with the reason ("turn on Share home folder in Settings → Agents → Safety, or open a
    project outside your home"). Linux/bwrap: not measured.
  - Catalog: `ConfigCatalog` reads `model`/`effort` from a scratch session and **deletes it** (opencode persists a session when it is
    created); the efforts of the current model only, since every model switch reloads the agent. The scratch directory is not
    resolved through symlinks (`/private/var/…` makes opencode resolve `/private`, which the jail does not show it).
  - **A provider outage explains itself** (`provider_retry_update`, `ProviderRetry`, `providerRetry.ts`). While its model
    provider fails, opencode retries and says so in `session_info_update` `_meta["opencode/retry"]` = `{attempt, nextRetryAt
    (RFC 3339), error:{type, message, status, response}}`, replaced on each attempt (~10 s apart) and `null` when that
    attempt starts (measured against the real 2.0.32 binary with a local provider answering 503: attempts 2..11 over
    ~80 s, then `session/prompt` fails with -32603 `"Internal error: <provider message>"`, data
    `{service:"session", errorName:"provider.internal"}`; and in the person's database: 128 such rows in one outage, each carrying the provider's whole
    response). A retry-only update parses as its own kind (`UpdateProviderRetry`, `Raw` dropped) and is session state: the
    recorder keeps **one row**, replaced in place (`sessionStateKinds`). `Service.endProviderRetry` clears a notice still
    showing when the turn ends (the agent does not always send the null before giving up). The window draws it as the
    session failure banner (`providerRetryFailure`: warning, counts down to `nextRetryAt`, no Dismiss) and as the rail row's
    second line (`providerRetries` in the store, fed by `AgentSessionsSubscriber`; an ask wins). The vendor does not say how
    many attempts it will make, so the UI never says "of N" (2.0.32 stopped after attempt 11). A new prompt does not clear it (its row sits above that prompt on
    a reload); a notice whose time passed 30 s ago is ignored (app quit mid-outage).
  - **A JSON-RPC error from a live process is a turn failure, not a crash** (`acpRuntime.Prompt`, shared with Claude).
    `refusedByLiveAgent`: the call returned an `*acp.RPCError` and the serve loop has not ended → `ErrTurnFailed` carrying the
    agent's sentence (`rpcSentence`: the message, or `data.details|message|error` when the message is the generic "Internal
    error", else the provider's own words from the last retry notice), the session stays idle with a "retry" banner
    (`agent-turn-failed`). A transport error, a closed pipe or an ended serve loop is still `ErrProcessFailed`; auth is still
    `ErrAgentAuthRequired`. Before this every non-auth error became "The agent stopped unexpectedly".
  - Not verified: a real model, `auth_required` as opencode really answers it, a subagent that calls tools (child-session
    updates are declared via `_meta["opencode/child-session-updates"]`, untested), `/compact` with a model that obeys its template,
    bwrap on Linux, Windows, and V1 (ACP is there too; only V2 was run).
- **A person's turn has no time limit; an agent's or terminal's does.** `Service.Invoke` applies
  `DefaultInvokeTimeout` (10 min) only when the caller is not the UI or a remote human
  (`invokeUnbounded`). It used to cut every UI turn at 10:00 exactly, which looked like the vendor
  stopping mid-task (no turn row, idle session, no message). A turn that does hit the limit now
  leaves a `timeout` session failure ("The turn stopped at its time limit").
- **A turn the vendor refuses is not a crash.** Codex's `usageLimitExceeded` →
  `ErrAgentUsageLimit` (`agent_usage_limit`); a full context window, a spent budget or
  any other failed turn → `ErrTurnFailed` (`agent_turn_failed`). Both carry Codex's own
  sentence (`VendorError.Message` — the only place that says when a limit lifts), leave
  the session `idle` rather than `error`, and are also reported as a session failure
  (`codexTurnFailure`), the banner Claude's air `sessionFailure` fills. The banner lasts
  one turn: the next prompt retires it (`transcript.ts`). `ErrProcessFailed` is for a
  process that actually died.

- **The prompt DevKit appends is rendered per vendor and per session**
  (`internal/agenthub/devkit_prompt.go`). `{{tool.name}}` placeholders become the names the
  model actually sees — `mcp__devkit__git_commit` for Claude, DevKit's own name plus a
  "your host may reformat it" note for Codex — and `TestPromptNamesOnlyRealTools` fails if
  one names no MCP tool. Normal CLI, search/LSP and skill workflows are allowed;
  CodeGraph and saving API requests are optional. Git writes still use DevKit's
  guarded tools; saved-connection checks still apply. `Service.SessionBrief`
  (`app.AgentSessionBrief`) puts the session's workspace folders and which it may write
  in the prompt instead of asking the agent to go and look.
- **Workspace instructions are read each turn, sent when they change.** Create/Edit workspace
  stores `instructions` locally (migration 34, UTF-8, max 32 KiB) together with its name and
  folders. `Service` reads them by the session's persisted `WorkspaceID` before invoking the
  runtime and builds a context block (instructions, memory index, session brief). The block is
  prepended to the prompt only when `workspaceContextDue` says the agent needs it: it has
  never received one, its fingerprint (SHA-256 of the text) differs from the last delivered
  one, or `workspaceContextRefreshTurns` (20) unchanged turns have passed (a vendor that
  compacts its conversation may have dropped it). A failed turn records nothing, so the next
  one carries the block again. The record is in memory and forgotten wherever the process
  holding the conversation is replaced: close, dead-runtime eviction, resume, rewind. Claude
  and Codex receive the same content; the user echo stays unchanged. Empty/unbound context
  retires earlier workspace instructions; DevKit's rules retain precedence. User-authored
  instructions are never passed through the tool-name placeholder renderer. The per-vendor
  prompt (`devkit_prompt.go`) tells the agent that a request without a block means the last
  one still holds.
- **Shared workspace memory is local and versioned** (`internal/workspacememory`,
  migration 35). `workspace.memory.*` tools scope Hub agents by their persisted
  workspace ID; terminal callers must supply an explicit ID. Update/delete/restore
  require `expectedRevision`; CAS and immutable history snapshots commit atomically.
  Source attribution comes from host identity, never tool input. The context block
  carries an index of at most 8 KiB (`Service.Context`): one `- id [kind] title — summary`
  line per entry (summary clipped to 140 runes, whitespace folded to one line, no bodies, no
  revisions), as reference data rather than instructions. The agent reads an entry with
  `workspace.memory.get`. The render is deterministic because agenthub fingerprints it;
  an empty workspace renders as "".
  Soft-deleted entries remain readable/restorable; deleting the workspace cascades
  entries and history. Plaintext SQLite storage is outside vault/sync, like workspace
  instructions. Audit excludes memory text and query. Workspace menu → Workspace memory
  opens the lazy manager: bounded summary pages, CAS edits and paged revision metadata
  with bodies loaded on selection. Migration 36 adds scoped contentless SQLite FTS5
  (`unicode61 remove_diacritics 2`) and materialized summary read models. Search is literal
  word-prefix AND, not substring; MCP list/search accept filter-bound cursors (legacy
  offset remains supported). `BenchmarkMemory50K` covers rare/common search, deep cursor
  pages and large revision histories. Broad searches still scale with matching postings.
- **Claude's PreToolUse hook refuses `git commit` / `git push` in Bash**
  (`cmd/devkit-mcp/hook.go`, `shellGitWrite`) with a reason naming the DevKit tool to call:
  the shell skips branch guards, the task trailer and the push rule. Other git commands
  are left alone. It reads plain spellings (`-C`, env prefixes, chains), not `sh -c`.

