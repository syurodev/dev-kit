import { describe, expect, test } from "bun:test";
import {
  bindPanelActions,
  closeStep,
  createSplitPanelStore,
  EMPTY_PANELS,
  openStep,
  releaseStep,
  shownPanels,
  type PanelsState,
} from "./splitPanelModel";

const noop = () => undefined;
const open = (s: PanelsState, owner: string, content: string, opener: string) =>
  openStep(s, owner, { content, width: 420, onClose: noop }, opener);

describe("a module's context pane: kept when closed, unless asked", () => {
  test("closing hides it and keeps its last content, so reopening renders in place", () => {
    const a = open(EMPTY_PANELS, "kafka", "message 1", "doc-1");
    const closed = closeStep(a, "kafka", false);
    expect(closed.panels.kafka).toMatchObject({ content: "message 1", hidden: true, generation: 0 });
    const again = open(closed, "kafka", "message 2", "doc-1");
    expect(again.panels.kafka).toMatchObject({ content: "message 2", hidden: false, generation: 0 });
  });

  test("closing what is already closed, or not there, changes nothing", () => {
    const closed = closeStep(open(EMPTY_PANELS, "kafka", "m", "doc-1"), "kafka", false);
    expect(closeStep(closed, "kafka", false)).toBe(closed);
    expect(closeStep(EMPTY_PANELS, "kafka", false)).toBe(EMPTY_PANELS);
    expect(closeStep(EMPTY_PANELS, "kafka", true)).toBe(EMPTY_PANELS);
  });

  test("discarding drops it, and the next open is a new tree even if it comes at once", () => {
    const a = open(EMPTY_PANELS, "tasks", "task 1", "board");
    const gone = closeStep(a, "tasks", true);
    expect(gone.panels.tasks).toBeUndefined();
    expect(open(gone, "tasks", "task 2", "board").panels.tasks.generation).toBe(1);
  });

  test("a kept pane reopened by another document is a new tree: its body is not that document's", () => {
    // Note A's history kept hidden, note B opens its own: no A versions on screen first.
    const kept = closeStep(open(EMPTY_PANELS, "notes", "history A", "editor-A"), "notes", false);
    expect(open(kept, "notes", "history B", "editor-B").panels.notes.generation).toBe(1);
  });

  test("while it is open, another document re-pushing it updates the same tree", () => {
    // Code Graph: choosing a reference opens another file, whose document shows the same panel.
    const a = open(EMPTY_PANELS, "codegraph", "refs", "file-1");
    expect(open(a, "codegraph", "refs", "file-2").panels.codegraph.generation).toBe(0);
  });

  test("the document that opened it unmounting drops it; anyone else unmounting does not", () => {
    const a = closeStep(open(EMPTY_PANELS, "codegraph", "inspector", "flow-1"), "codegraph", false);
    expect(releaseStep(a, "codegraph", "file-9")).toBe(a);
    const released = releaseStep(a, "codegraph", "flow-1");
    expect(released.panels.codegraph).toBeUndefined();
    expect(released.gens.codegraph).toBe(1);
  });

  test("only open panes are shown", () => {
    const s = closeStep(open(open(EMPTY_PANELS, "a", "x", "1"), "b", "y", "2"), "b", false);
    expect(Object.keys(shownPanels(s.panels))).toEqual(["a"]);
    expect(shownPanels(open(EMPTY_PANELS, "a", "x", "1").panels)).toEqual(open(EMPTY_PANELS, "a", "x", "1").panels);
  });
});

describe("the split-panel store: actions that never change, state that does", () => {
  test("the actions object and each document's bound actions keep their identity across every toggle", () => {
    // Documents (FileDocument, FlowDocument, NoteEditor…) list these in effect
    // dependencies and must not re-render when any pane opens or closes.
    const store = createSplitPanelStore<string>();
    const actions = store.actions;
    const bound = bindPanelActions(store.actions, "kafka", "doc-1");
    const { openPanel, closePanel, openCompanion, closeCompanion } = bound;
    let notified = 0;
    store.subscribe(() => notified++);
    openPanel("message", { onClose: noop });
    openCompanion("agent", { onClose: noop });
    closePanel();
    openCompanion("agent", { onClose: noop, hidden: true });
    closeCompanion();
    expect(notified).toBe(5);
    expect(store.actions).toBe(actions);
    expect([bound.openPanel, bound.closePanel, bound.openCompanion, bound.closeCompanion]).toEqual([
      openPanel,
      closePanel,
      openCompanion,
      closeCompanion,
    ]);
  });

  test("bound actions open and close the caller's own pane, hidden by default, dropped on discard or release", () => {
    const store = createSplitPanelStore<string>();
    const doc = bindPanelActions(store.actions, "notes", "editor-A");
    doc.openPanel("meta", { width: 400, onClose: noop });
    expect(store.getState().panels.notes).toMatchObject({ content: "meta", width: 400, opener: "editor-A" });
    doc.closePanel();
    expect(store.getState().panels.notes.hidden).toBe(true);
    doc.closePanel({ discard: true });
    expect(store.getState().panels.notes).toBeUndefined();
    doc.openPanel("meta", { onClose: noop });
    expect(store.getState().panels.notes.width).toBe(420);
    store.actions.releasePanelFor("notes", "editor-A");
    expect(store.getState().panels.notes).toBeUndefined();
  });

  test("a change that changes nothing tells no one", () => {
    const store = createSplitPanelStore<string>();
    let notified = 0;
    store.subscribe(() => notified++);
    store.actions.closePanelFor("kafka");
    store.actions.releasePanelFor("kafka", "doc-1");
    store.actions.closeCompanion();
    expect(notified).toBe(0);
  });
});
