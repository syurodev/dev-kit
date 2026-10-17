/** A page in the left nav: one concern each. */
export type SettingsPage =
  | "account" | "appearance" | "sync" | "environments" | "database" | "keyboard" | "security" | "updates"
  | "agents-runtimes" | "agents-models" | "agents-skills" | "agents-safety" | "agents-notifications" | "agents-mcp";

/**
 * Ids that used to be pages. Callers (sign-in and not-installed prompts, saved
 * deep links) still pass them; they land on the page that took the content over.
 */
export type LegacySettingsTab = "agents" | "remote" | "mcp";

/** What a caller may ask Settings to open. */
export type SettingsTab = SettingsPage | LegacySettingsTab;

const LEGACY_TABS: Record<LegacySettingsTab, SettingsPage> = {
  agents: "agents-runtimes",
  remote: "agents-notifications",
  mcp: "agents-mcp",
};

/** The page a requested tab opens: an old id maps to its successor, a page to itself. */
export function resolveSettingsTab(tab: SettingsTab): SettingsPage {
  return tab in LEGACY_TABS ? LEGACY_TABS[tab as LegacySettingsTab] : (tab as SettingsPage);
}

export const SETTINGS_GROUPS = [
  { label: "Personal", items: [
    { id: "appearance", label: "Appearance", keywords: "theme palette font terminal tabs size" },
    { id: "keyboard", label: "Keyboard", keywords: "shortcuts keys" },
    { id: "security", label: "Security", keywords: "vault password credentials reveal" },
    { id: "updates", label: "Updates", keywords: "version release application" },
  ] },
  { label: "Workspace", items: [
    { id: "environments", label: "Environments", keywords: "prod staging confirmation tags" },
    { id: "database", label: "Database", keywords: "query console storage files" },
  ] },
  { label: "Agents", items: [
    { id: "agents-runtimes", label: "Runtimes", keywords: "claude codex install version node sign in login api key observed sessions" },
    { id: "agents-models", label: "Models & subagents", keywords: "model effort reasoning profiles jobs subagent delegate" },
    { id: "agents-skills", label: "Skills & MCP", keywords: "tools skills mcp servers import folders override disable customization" },
    { id: "agents-safety", label: "Safety", keywords: "sandbox checkpoints rollback docker home folder isolation permissions" },
    { id: "agents-notifications", label: "Notifications & remote", keywords: "notify alerts telegram bot pairing phone approvals remote control" },
    { id: "agents-mcp", label: "DevKit as MCP", keywords: "tools proxy install agent config claude code codex integration" },
  ] },
  { label: "Services", items: [
    { id: "account", label: "Account", keywords: "sign in identity logout" },
    { id: "sync", label: "Sync", keywords: "devices endpoint encrypted" },
  ] },
] as const satisfies readonly { label: string; items: readonly { id: SettingsPage; label: string; keywords: string }[] }[];

/** Filter navigation only: opening Settings never fetches every section. */
export function filterSettingsNavigation(query: string) {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return SETTINGS_GROUPS.map((group) => ({ ...group, items: group.items.filter((item) =>
    words.every((word) => `${group.label} ${item.label} ${item.keywords}`.toLowerCase().includes(word)),
  ) })).filter((group) => group.items.length > 0);
}
