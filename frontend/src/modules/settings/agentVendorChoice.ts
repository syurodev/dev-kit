/**
 * Which vendor the Claude | Codex switch on the Agents pages was last left on.
 * One answer for every page that has the switch, so Skills & MCP opens on the
 * vendor Runtimes was just used for. Storage can be unavailable or hold junk:
 * reads fall back to the first vendor offered and a failed write is only not remembered.
 */
export const AGENT_VENDOR_KEY = "devkit.settings.agents.vendor";

/** The remembered vendor if it is one of `offered`, else the first of them. */
export function readAgentVendor(offered: readonly string[]): string {
  try {
    const stored = localStorage.getItem(AGENT_VENDOR_KEY);
    if (stored && offered.includes(stored)) return stored;
  } catch {
    // Unavailable: start on the first.
  }
  return offered[0] ?? "";
}

export function writeAgentVendor(vendor: string): void {
  try {
    localStorage.setItem(AGENT_VENDOR_KEY, vendor);
  } catch {
    // Not remembered; the switch still works for this window.
  }
}
