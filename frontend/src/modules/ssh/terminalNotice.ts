import type { TerminalExitReason } from "@/stores/terminalTabs";

export function terminalExitNotice(code: number, reason?: TerminalExitReason): string {
  if (reason === "closed") return "";
  if (code === 0) return "Remote terminal session ended.";
  if (code === -1) return "Connection to the remote terminal was lost. Open a new terminal to reconnect.";
  return `Remote terminal stopped (exit code ${code}). Open a new terminal to reconnect.`;
}
