import { describe, expect, test } from "bun:test";
import {
  closeStep,
  createSplitPanelStore,
  EMPTY_PANELS,
  forgetHiddenStep,
  openStep,
  releaseStep,
  shownPanels,
  type PanelsState,
} from "./splitPanelModel";

const noop = () => undefined;
const open = (s: PanelsState, owner: string, content: string, opener: string, key?: string) =>
  openStep(s, owner, { content, width: 420, onClose: noop, key }, opener);

describe("a module's context pane: kept when closed, unless asked", () => {
  test("closing hides it and keeps its last content, so reopening renders in place", () => {
    const a = open(EMPTY_PANELS, "kafka", "message 1", "doc-1", "message");
    const closed = closeStep(a, "kafka", false, "doc-1");
    expect(closed.panels.kafka).toMatchObject({ content: "message 1", hidden: true, generation: 0 });
    const again = open(closed, "kafka", "message 2", "doc-1", "message");
    expect(again.panels.kafka).toMatchObject({ content: "message 2", hidden: false, generation: 0 });
  });

  test("closing what is already closed, or not there, changes nothing", () => {
    const closed = closeStep(open(EMPTY_PANELS, "kafka", "m", "doc-1"), "kafka", false, "doc-1");
    expect(closeStep(closed, "kafka", false, "doc-1")).toBe(closed);
    expect(closeStep(EMPTY_PANELS, "kafka", false, "doc-1")).toBe(EMPTY_PANELS);
    expect(closeStep(EMPTY_PANELS, "kafka", true, "doc-1")).toBe(EMPTY_PANELS);
  });

  test("a close from a caller that is not the one showing the pane is ignored", () => {
    // Two documents of one module: the one in the back must not close the front one's pane.
    const a = open(EMPTY_PANELS, "codegraph", "inspector", "flow-2", "flow:b");
    expect(closeStep(a, "codegraph", false, "flow-1")).toBe(a);
    expect(closeStep(a, "codegraph", true, "flow-1")).toBe(a);
  });

  test("discarding drops it, and the next open is a new tree even if it comes at once", () => {
    const a = open(EMPTY_PANELS, "tasks", "task 1", "board", "task:1");
    const gone = closeStep(a, "tasks", true, "board");
    expect(gone.panels.tasks).toBeUndefined();
    expect(open(gone, "tasks", "task 1", "board", "task:1").panels.tasks.generation).toBe(1);
  });

  test("the body is identified by its key: a different key is a new tree, open or hidden", () => {
    // Note A's metadata kept hidden, note B opens its own: none of A's on screen first.
    const kept = closeStep(open(EMPTY_PANELS, "notes", "meta A", "editor-A", "meta:A"), "notes", false, "editor-A");
    expect(open(kept, "notes", "meta B", "editor-B", "meta:B").panels.notes.generation).toBe(1);
    const shown = open(EMPTY_PANELS, "codegraph", "flow a", "flow-1", "flow:a");
    expect(open(shown, "codegraph", "flow b", "flow-2", "flow:b").panels.codegraph.generation).toBe(1);
  });

  test("choosing a reference: the document closes, another reopens the same lookup — one tree throughout", () => {
    // FileDocument A loses the front (its effect closes the pane), B gets it
    // and opens the same store panel, then A unmounts (its tab was a preview).
    let s = open(EMPTY_PANELS, "codegraph", "refs", "file-A", "nav");
    s = closeStep(s, "codegraph", false, "file-A");
    s = open(s, "codegraph", "refs", "file-B", "nav");
    s = releaseStep(s, "codegraph", "file-A");
    expect(s.panels.codegraph).toMatchObject({ hidden: false, opener: "file-B", generation: 0 });
  });

  test("the caller that opened it unmounting drops it; anyone else unmounting does not", () => {
    const a = closeStep(open(EMPTY_PANELS, "codegraph", "inspector", "flow-1"), "codegraph", false, "flow-1");
    expect(releaseStep(a, "codegraph", "file-9")).toBe(a);
    const released = releaseStep(a, "codegraph", "flow-1");
    expect(released.panels.codegraph).toBeUndefined();
    expect(released.gens.codegraph).toBe(1);
  });

  test("hidden panes of modules no longer on screen are dropped; open ones and visible ones stay", () => {
    let s = open(EMPTY_PANELS, "kafka", "m", "k");
    s = closeStep(s, "kafka", false, "k");
    s = open(s, "notes", "meta", "n");
    s = open(s, "codegraph", "nav", "c");
    s = closeStep(s, "codegraph", false, "c");
    const next = forgetHiddenStep(s, ["codegraph"]);
    expect(Object.keys(next.panels).sort()).toEqual(["codegraph", "notes"]);
    expect(forgetHiddenStep(next, ["codegraph"])).toBe(next);
  });

  test("only open panes are shown", () => {
    const s = closeStep(open(open(EMPTY_PANELS, "a", "x", "1"), "b", "y", "2"), "b", false, "2");
    expect(Object.keys(shownPanels(s.panels))).toEqual(["a"]);
    const none = open(EMPTY_PANELS, "a", "x", "1").panels;
    expect(shownPanels(none)).toBe(none);
  });
});

describe("the split-panel store", () => {
  const deferred = () => {
    const queue: (() => void)[] = [];
    return { defer: (fn: () => void) => void queue.push(fn), flush: () => queue.splice(0).forEach((fn) => fn()) };
  };

  test("a caller's bound actions are one object for its lifetime, routed to its own module", () => {
    // What the hook returns on every render: documents list these in effect
    // dependencies and must not re-run them, or re-render, when a pane toggles.
    const store = createSplitPanelStore<string>();
    const doc = store.bound("kafka", "doc-1");
    doc.openPanel("message", { onClose: noop });
    store.actions.openCompanion("agent", { onClose: noop });
    doc.closePanel();
    store.actions.closeCompanion();
    expect(store.bound("kafka", "doc-1")).toBe(doc);
    const other = store.bound("notes", "doc-1");
    expect(other).not.toBe(doc);
    other.openPanel("meta", { onClose: noop });
    expect(store.getState().panels.notes.content).toBe("meta");
    expect(store.getState().panels.kafka).toMatchObject({ content: "message", hidden: true });
  });

  test("bound actions: hidden by default, dropped on discard, a key and a width when given", () => {
    const store = createSplitPanelStore<string>();
    const doc = store.bound("notes", "editor-A");
    doc.openPanel("meta", { width: 400, onClose: noop, key: "meta:A" });
    expect(store.getState().panels.notes).toMatchObject({ content: "meta", width: 400, opener: "editor-A", key: "meta:A" });
    doc.closePanel();
    expect(store.getState().panels.notes.hidden).toBe(true);
    doc.closePanel({ discard: true });
    expect(store.getState().panels.notes).toBeUndefined();
    doc.openPanel("meta", { onClose: noop });
    expect(store.getState().panels.notes.width).toBe(420);
  });

  test("detaching a caller drops its pane a moment later, unless it came back (StrictMode) or another took it", () => {
    const { defer, flush } = deferred();
    const store = createSplitPanelStore<string>({ defer });
    const a = store.bound("codegraph", "file-A");
    store.attach("codegraph", "file-A");
    a.openPanel("refs", { onClose: noop, key: "nav" });
    // StrictMode's rehearsal: unmount and mount again in one flush.
    store.detach("codegraph", "file-A");
    store.attach("codegraph", "file-A");
    flush();
    expect(store.getState().panels.codegraph).toBeDefined();
    // Another document took the pane before A left.
    store.bound("codegraph", "file-B").openPanel("refs", { onClose: noop, key: "nav" });
    store.detach("codegraph", "file-A");
    flush();
    expect(store.getState().panels.codegraph).toMatchObject({ opener: "file-B", generation: 0 });
    // B leaves for good.
    store.detach("codegraph", "file-B");
    flush();
    expect(store.getState().panels.codegraph).toBeUndefined();
  });

  test("a change that changes nothing tells no one", () => {
    const store = createSplitPanelStore<string>();
    let notified = 0;
    store.subscribe(() => notified++);
    store.actions.closePanelFor("kafka", "doc-1");
    store.actions.releasePanelFor("kafka", "doc-1");
    store.actions.forgetHiddenPanels(["kafka"]);
    store.actions.closeCompanion();
    expect(notified).toBe(0);
  });
});
