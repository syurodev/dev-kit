import { create } from "zustand";
import { errorMessage } from "@/bridge/errors";

export type DiffMode = "split" | "unified";
export type GitDraft = {
  message: string;
  historySearch: string;
  historyLimit: number;
};
const EMPTY_DRAFT: GitDraft = {
  message: "",
  historySearch: "",
  historyLimit: 100,
};
type Operation = { label: string; error: string; notice: string };
const EMPTY_OPERATION: Operation = { label: "", error: "", notice: "" };

/** Session-only drafts/results; closing a document does not cancel a Git write. */
export function createGitUI() {
  const useStore = create<{
    drafts: Record<string, GitDraft>;
    operations: Record<string, Operation>;
    diffMode: DiffMode | null;
  }>(() => ({ drafts: {}, operations: {}, diffMode: null }));
  function setDraft(folder: string, patch: Partial<GitDraft>) {
    useStore.setState((s) => ({
      drafts: {
        ...s.drafts,
        [folder]: { ...EMPTY_DRAFT, ...s.drafts[folder], ...patch },
      },
    }));
  }
  async function run(folder: string, label: string, work: () => Promise<unknown>, notice = "") {
    if (Object.values(useStore.getState().operations).some((op) => op.label)) return false;
    const put = (op: Operation) =>
      useStore.setState((s) => ({
        operations: { ...s.operations, [folder]: op },
      }));
    put({ label, error: "", notice: "" });
    try {
      await work();
      put({ label: "", error: "", notice });
      return true;
    } catch (e) {
      put({ label: "", error: errorMessage(e), notice: "" });
      return false;
    }
  }
  function clearFeedback(folder: string) {
    if (useStore.getState().operations[folder]?.label) return;
    useStore.setState((s) => ({
      operations: { ...s.operations, [folder]: EMPTY_OPERATION },
    }));
  }
  return { useStore, setDraft, run, clearFeedback };
}
export const gitUI = createGitUI();
export function useGitDraft(folder: string) {
  return gitUI.useStore((s) => s.drafts[folder] ?? EMPTY_DRAFT);
}
export function useGitOperation(folder: string) {
  return gitUI.useStore((s) => s.operations[folder] ?? EMPTY_OPERATION);
}
export function useGitBusy() {
  return gitUI.useStore((s) => Object.values(s.operations).some((op) => !!op.label));
}
export function filterCommits<T extends { sha: string; subject: string; author: string; body: string }>(
  commits: T[],
  search: string,
): T[] {
  const words = search.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return commits.filter((commit) =>
    words.every((word) =>
      `${commit.sha} ${commit.subject} ${commit.author} ${commit.body}`.toLowerCase().includes(word),
    ),
  );
}
