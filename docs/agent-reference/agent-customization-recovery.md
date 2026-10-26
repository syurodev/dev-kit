### Agent customization and recovery
- **Settings → Agents is its own nav group of seven one-topic pages**, not one tab
  (`frontend/src/modules/settings/settingsNavigation.ts` is the model, `AgentsPages.tsx` the pages):
  Runtimes (`agents-runtimes`, + Codex observed sessions), Models & subagents (`agents-models`),
  Accounts & limits (`agents-accounts`, several logins per vendor and failover, see **Accounts UI**),
  Skills & MCP (`agents-skills`, the tools/skills/MCP report and `AgentCustomization` overrides,
  `AgentEnvironmentPanel`), Safety (`agents-safety`, sandbox + file checkpoints), Notifications & remote
  (`agents-notifications`, + Telegram) and DevKit as MCP (`agents-mcp`). Runtimes and Skills & MCP carry a shadcn
  `Tabs` Claude | Codex | opencode switch (Accounts & limits has Claude | Codex only) whose last choice is shared and kept in `localStorage`
  (`agentVendorChoice.ts`, guarded); the other pages are vendor-neutral. The old ids `agents`, `remote` and `mcp`
  stay valid for callers (`requestSettings("agents")`): `resolveSettingsTab` maps them to Runtimes, Notifications &
  remote and DevKit as MCP. A new agent setting is a panel on the page whose topic it is, plus keywords on that
  page's `SETTINGS_GROUPS` entry — not a new stacked section.
- Settings → Agents → Tools, skills and MCP manages local imports and per-vendor
  overrides (`preferences.AgentCustomization`, in the existing sandbox preference
  JSON). Dedicated UI-only get/set bridge methods use revision/CAS. General sandbox
  saves preserve this map; they cannot overwrite it from a stale Settings form.
- Imported skill folders are read-only links, never copied or deleted at source.
  Codex uses native skill config (both canonical file and folder forms for runtime
  compatibility); Claude uses a temporary local plugin cleaned up with its process.
  MCP definitions support stdio and HTTP, with environment variable names only.
- Claude's inherited toggles deny skill/MCP tool invocation, not MCP startup.
  Vendor source deny rules remain authoritative. Local source updates are applied
  by Refresh + Reload; no remote installer/update command runs automatically.
- Resume/Reload is UI-only and reuses the persisted native conversation ID without
  replaying a prompt. Reload reserves the session against Invoke and resume while
  closing its idle runtime. A missing native conversation stays an explicit error.
  Startup recovery adds a durable interruption notice; model/effort/mode are restored
  before attaching live handlers. User-requested Retry turn remains separate.


## Effective harness configuration (`internal/harness`)

`internal/harness` is the one definition of a session's effective configuration (tool
groups, result budgets, deny lists, servers, skill folders). It is a stdlib-only leaf
(`internal/arch` enforces it): the registry, the vendor customization (V), the vendor's own
environment facts, the workspace profile (W), the session override (S) and path
canonicalization are all injected, so nothing else may re-implement the merge. H1 ships the
contract only; no service calls it yet and behavior is unchanged.

- `Resolve(Inputs)` returns an `Effective` or a typed `*harness.Error`. An invalid input is
  rejected, never skipped: a failure means "do not open", not "fall back to unfiltered".
  With no W and no S every group is on at the 24 KiB default (`mcp.ResultBudget` is pinned
  equal to `harness.DefaultResultBudget`).
- Absent vs present-empty: nil vs empty non-nil with `omitzero`. W and S carry a schema
  version and a revision; a missing W or S is a nil revision, never 0.
- Groups: absent is on, W can turn one off, S can only turn one on; unknown groups are
  `harness.groupUnknown`. `harness.profile.propose` is the one tool served with its group off.
- Budgets: tool, then group, then default, each of size and cut separately; 2–64 KiB;
  `budgetExempt` tools cannot be overridden. `headTail` is rejected (`harness.budgetInvalid`)
  until phase 3 implements it.
- Deny lists are V ∪ W. Servers are keyed by name, W replaces V, a duplicate or a name the
  vendor environment already defines is rejected. Skill folders are V ∪ W by canonical path.
- `Classify(before, after)` compares two effective values: executable > widening > narrowing.
  A deny entry whose removal exposes a vendor-defined server or skill, or an imported skill
  folder, is *executable* (a denied imported skill is not in the executable set); a
  cut-mode-only change is neutral in the narrowing tier; budgets compare per tool on the
  effective value (deleting a 16 KiB override widens).
- `Effective.Fingerprint` (`sha256:v1:`) covers the V/W/S revisions, the registry's tool names
  and exemptions, the vendor-defined servers/skills (name, command, args, URL, env names,
  sorted) and the canonical targets of imported skill folders and denied skills, so re-pointing
  a symlink moves it. Nil and empty args/env names are one value (storage drops empty lists).
  No env values, no definition sizes.
- Known gaps for later slices: `EnvironmentReport` entries carry no command/arguments, so
  the host must add them before a vendor-defined server can be fingerprinted (spike 9); a stored
  profile naming a tool or group that a later build removed makes `Resolve` fail, so H2/H5 need
  a repair path.
