import { create } from "zustand";
import { errorMessage } from "@/bridge/errors";
import type { SSHForwardSpec } from "@/bridge/ssh";

export type SSHPage = "terminal" | "commands" | "tunnels" | "details";
export type FeedbackScope = SSHPage | "connection" | "approval";
export type WorkspaceState = {
  page: SSHPage; command: string; output: string | null; busy: string;
  error: string; status: string; scope: FeedbackScope;
  spec: Omit<SSHForwardSpec, "listenPort" | "targetPort"> & { listenPort: number | string; targetPort: number | string };
  forwardErrors: Record<string, string>;
  approval: { fingerprint: string; connect: boolean } | null;
  terminalNotice: string;
};
export const initialWorkspace: WorkspaceState = {
  forwardErrors: {}, approval: null, terminalNotice: "",
  page: "terminal", command: "hostname", output: null, busy: "", error: "", status: "", scope: "connection",
  spec: { direction: "local", listenHost: "127.0.0.1", listenPort: 0, targetHost: "127.0.0.1", targetPort: 5432 },
};

/** Session memory only: no command/output is written to UI preferences. */
export const useSSHWorkspaceState = create<{
  entries: Record<string, WorkspaceState>;
  patch: (id: string, update: Partial<WorkspaceState>) => void;
  forget: (id: string) => void;
}>((set) => ({
  entries: {},
  patch: (id, update) => set(s => ({ entries: { ...s.entries, [id]: { ...(s.entries[id] ?? initialWorkspace), ...update } } })),
  forget: id => set(s => { const entries = { ...s.entries }; delete entries[id]; return { entries }; }),
}));

/** Claim synchronously, before React renders, so rapid clicks cannot double-run. */
export function beginSSHAction(id: string, busy: string, scope: FeedbackScope): boolean {
  const store = useSSHWorkspaceState.getState();
  if (store.entries[id]?.busy) return false;
  store.patch(id, { busy, scope, error: "", status: "" });
  return true;
}

export async function runSSHAction(id: string, label: string, scope: FeedbackScope, work: () => Promise<void>): Promise<"success" | "error" | "busy"> {
  if (!beginSSHAction(id, label, scope)) return "busy";
  const patch = useSSHWorkspaceState.getState().patch;
  try {
    await work();
    return "success";
  } catch (error) {
    patch(id, { error: errorMessage(error) });
    return "error";
  } finally {
    patch(id, { busy: "" });
  }
}
