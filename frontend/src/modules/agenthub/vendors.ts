import { CLAUDE_VENDOR, CODEX_VENDOR } from "@/bridge/agenthub";
import { CLAUDE_AGENT, CODEX_AGENT } from "@/bridge/agentRuntime";

/**
 * How a vendor signs in. Claude runs its own login in a terminal the person
 * watches; Codex signs in inside DevKit (a page or a code, then an event);
 * anything else — the mock adapter — has nothing to sign in to.
 */
export type VendorSignIn = "terminal" | "inApp" | "none";

/**
 * What the UI needs to know about an agent it did not hard-code. Every place
 * that used to compare against "claude" reads this instead, so a second
 * vendor is a row here rather than a search through the module.
 */
export type VendorInfo = {
  /** The agentType a session is created with. */
  id: string;
  /** What the person calls it: "Ask Claude…", "Codex isn't signed in". */
  name: string;
  /** The runtime DevKit installs for it (bridge/agentRuntime), or "" for none. */
  runtimeId: string;
  /** The runtime's name in Settings and the install panel. */
  runtimeName: string;
  signIn: VendorSignIn;
  /** Can read model settings without creating a conversation. */
  configCatalog?: boolean;
};

export const CLAUDE_INFO: VendorInfo = {
  id: CLAUDE_VENDOR,
  name: "Claude",
  runtimeId: CLAUDE_AGENT,
  runtimeName: "Claude Agent",
  signIn: "terminal",
};

export const CODEX_INFO: VendorInfo = {
  id: CODEX_VENDOR,
  name: "Codex",
  runtimeId: CODEX_AGENT,
  runtimeName: "Codex",
  signIn: "inApp",
  configCatalog: true,
};

const KNOWN: Record<string, VendorInfo> = {
  [CLAUDE_INFO.id]: CLAUDE_INFO,
  [CODEX_INFO.id]: CODEX_INFO,
};

/** The vendors DevKit ships, in the order the picker offers them. */
export const SHIPPED_VENDORS: readonly string[] = [CLAUDE_INFO.id, CODEX_INFO.id];

/**
 * A vendor's row. One DevKit does not know gets its id for a name and nothing
 * to install or sign in to — never Claude's row, which is what defaulting to
 * "claude" used to do.
 */
export function vendorInfo(id: string | null | undefined): VendorInfo {
  const key = (id ?? "").trim();
  const known = KNOWN[key.toLowerCase()];
  if (known) return known;
  const name = key ? key.charAt(0).toUpperCase() + key.slice(1) : "Agent";
  return { id: key, name, runtimeId: "", runtimeName: name, signIn: "none" };
}
