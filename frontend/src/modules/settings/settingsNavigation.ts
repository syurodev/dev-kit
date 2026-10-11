export type SettingsTab = "account" | "appearance" | "sync" | "environments" | "database" | "agents" | "keyboard" | "security" | "updates" | "mcp" | "remote";

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
  { label: "Services", items: [
    { id: "account", label: "Account", keywords: "sign in identity logout" },
    { id: "sync", label: "Sync", keywords: "devices endpoint encrypted" },
    { id: "agents", label: "Agents", keywords: "claude codex runtime sandbox checkpoints notifications" },
    { id: "remote", label: "Remote control", keywords: "telegram bot pairing phone approvals" },
    { id: "mcp", label: "MCP integrations", keywords: "tools proxy install agent config" },
  ] },
] as const satisfies readonly { label: string; items: readonly { id: SettingsTab; label: string; keywords: string }[] }[];

/** Filter navigation only: opening Settings never fetches every section. */
export function filterSettingsNavigation(query: string) {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return SETTINGS_GROUPS.map((group) => ({ ...group, items: group.items.filter((item) =>
    words.every((word) => `${group.label} ${item.label} ${item.keywords}`.toLowerCase().includes(word)),
  ) })).filter((group) => group.items.length > 0);
}
