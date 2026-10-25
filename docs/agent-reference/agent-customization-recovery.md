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

