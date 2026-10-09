import { create } from "zustand";
import { focusedGroup } from "@/stores/documentGroups";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import {
  back as historyBack,
  canBack,
  canForward,
  capture,
  emptyHistory,
  forward as historyForward,
  push,
  type NavHistory,
  type NavPlace,
  type Step,
} from "./workspace/navigationHistory";
import { docKey } from "./workspace/identity";
import { useCodeGraphStore } from "./codegraphStore";
import { focusedDocument } from "./documents";

/**
 * Back / Forward state for the editor groups (workspace/navigationHistory.ts
 * has the rules). One history per group, as in VS Code: two groups side by side
 * are two trails, and neither reads the other's.
 */
export const useNavigationStore = create<{ histories: Record<string, NavHistory> }>(() => ({ histories: {} }));

const histories = () => useNavigationStore.getState().histories;
function setHistory(groupId: string, history: NavHistory) {
  useNavigationStore.setState((s) => ({ histories: { ...s.histories, [groupId]: history } }));
}

/** Forgets the histories of groups that no longer exist. */
export function pruneHistories(groupIds: readonly string[]) {
  const alive = new Set(groupIds);
  const { histories: all } = useNavigationStore.getState();
  const kept = Object.fromEntries(Object.entries(all).filter(([id]) => alive.has(id)));
  if (Object.keys(kept).length !== Object.keys(all).length) useNavigationStore.setState({ histories: kept });
}

// ── The live editors ────────────────────────────────────────────────────────

/**
 * What each mounted editor can report about itself, by docKey. The caret and
 * the scroll position live in the editor, not in the store (they change on
 * every keystroke and click), so a navigation asks for them at the moment the
 * place is being left.
 */
export type EditorHandle = {
  probe: () => { line: number; column: number; anchor: number; head: number; scrollTop: number } | null;
  focus: () => void;
};
const editors = new Map<string, EditorHandle>();

export function registerEditor(key: string, handle: EditorHandle): () => void {
  editors.set(key, handle);
  return () => {
    if (editors.get(key) === handle) editors.delete(key);
  };
}

/** Puts the caret back in a document's editor — after a peek is closed, say. */
export function focusEditor(key: string): void {
  editors.get(key)?.focus();
}

/** The place the focused group is showing right now, or null when it shows no file. */
export function hereNow(): { groupId: string; place: NavPlace } | null {
  const front = focusedDocument();
  const d = front?.doc;
  if (!front || !d || d.kind !== "file" || !d.workspaceId || !d.folderId || !d.path) return null;
  const key = docKey(d.workspaceId, d.folderId, d.path);
  const probed = editors.get(key)?.probe();
  const buffer = useCodeGraphStore.getState().buffers[key];
  const place: NavPlace = probed
    ? { workspaceId: d.workspaceId, folderId: d.folderId, path: d.path, line: probed.line, column: probed.column, anchor: probed.anchor, head: probed.head, scrollTop: probed.scrollTop }
    : { workspaceId: d.workspaceId, folderId: d.folderId, path: d.path, line: buffer?.line ?? 1, column: 1 };
  return { groupId: front.groupId, place };
}

/** Notes where the focused group is before it leaves for somewhere else on purpose. */
export function captureDeparture(): void {
  const here = hereNow();
  if (!here) return;
  setHistory(here.groupId, capture(histories()[here.groupId] ?? emptyHistory(), here.place));
}

/** Records a place a group has arrived at. */
export function recordArrival(groupId: string, place: NavPlace): void {
  setHistory(groupId, push(histories()[groupId] ?? emptyHistory(), place));
}

/** The focused group's id, after an open has put its tab there. */
export function focusedGroupId(): string {
  const state = useWorkbenchTabsStore.getState();
  return focusedGroup(state)?.id ?? "";
}

/**
 * The step Back or Forward would take from where the focused group is, with
 * the history that results — or null. `usable` lets the caller skip places that
 * are gone.
 */
export function planStep(dir: "back" | "forward", usable: (p: NavPlace) => boolean): { groupId: string; step: Step } | null {
  const groupId = focusedGroupId();
  if (!groupId) return null;
  const here = hereNow();
  let h = histories()[groupId] ?? emptyHistory();
  if (here && here.groupId === groupId) h = capture(h, here.place);
  const step = (dir === "back" ? historyBack : historyForward)(h, usable);
  return step ? { groupId, step } : null;
}

/** Commits a step once its target has actually opened. */
export function commitStep(groupId: string, step: Step) {
  setHistory(groupId, step.history);
}

/** For the toolbar: whether the focused group can go Back / Forward. */
export function useCanNavigate(): { back: boolean; forward: boolean } {
  const groupId = useWorkbenchTabsStore((s) => focusedGroup(s)?.id ?? "");
  const h = useNavigationStore((s) => s.histories[groupId]);
  return { back: h ? canBack(h) : false, forward: h ? canForward(h) : false };
}
