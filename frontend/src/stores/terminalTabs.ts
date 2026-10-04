import { create } from "zustand";

export type TerminalTab = {
  /** Client-side identity, assigned before the PTY exists. */
  key: string;
  title: string;
  /** SSH connection ownership, so edits/deletion can close its tabs. */
  profileId?: string;
  /** Starts this tab's process; a shell at the open project when absent. */
  start?: (rows: number, cols: number) => Promise<string>;
  /** The process's exit code, once; -1 when the tab closed first. */
  onExit?: (code: number) => void;
};

/** A tab that runs one command instead of a shell, e.g. Agent Hub's sign-in. */
export type TerminalCommandTab = Pick<
  TerminalTab,
  "title" | "start" | "onExit" | "profileId"
>;

/**
 * Which tab should take over when `closing` goes away.
 *
 * Pure because the interesting part is entirely about ordering: closing the
 * active tab should land on its neighbour rather than snapping to the first,
 * and closing a background tab must not steal focus.
 */
export function nextActiveKey(
  tabs: TerminalTab[],
  activeKey: string,
  closing: string,
): string {
  if (activeKey !== closing) return activeKey;
  const index = tabs.findIndex((t) => t.key === closing);
  const remaining = tabs.filter((t) => t.key !== closing);
  if (remaining.length === 0) return "";
  // Prefer the one on the right, which is where the eye already is; fall back
  // to the left when the closed tab was last.
  return (remaining[index] ?? remaining[remaining.length - 1]).key;
}

type TerminalTabsState = {
  tabs: TerminalTab[];
  activeKey: string;
  /** Monotonic, so a reused index never gives two shells the same title. */
  nextId: number;
  openTab: (command?: TerminalCommandTab) => void;
  closeTab: (key: string) => void;
  setActive: (key: string) => void;
};

/**
 * Terminal tabs live in the shell, not in a module.
 *
 * A shell rooted at a project should outlive a glance at another module —
 * leaving CodeGraph must not kill a running build. Module-scoped state cannot
 * do that: leaving a module unmounts its subtree.
 */
export const useTerminalTabsStore = create<TerminalTabsState>((set) => ({
  tabs: [],
  activeKey: "",
  nextId: 1,
  openTab: (command) =>
    set((s) => {
      const tab: TerminalTab = {
        title: `Terminal ${s.nextId}`,
        ...command,
        key: `t${s.nextId}`,
      };
      return {
        tabs: [...s.tabs, tab],
        activeKey: tab.key,
        nextId: s.nextId + 1,
      };
    }),
  closeTab: (key) =>
    set((s) => ({
      tabs: s.tabs.filter((t) => t.key !== key),
      activeKey: nextActiveKey(s.tabs, s.activeKey, key),
    })),
  setActive: (key) => set({ activeKey: key }),
}));
