import { expect, test, beforeEach } from "bun:test";
import { createElement } from "react";
import { useBottomContentStore } from "./bottomContent";

// Real elements, not plain objects: the store's value type is ReactNode, and
// createElement returns a fresh object every call — so `toBe` below is testing
// identity, which is exactly what the store's bail-out compares. A string
// would typecheck too but make the identity test vacuous.
const panel = (marker: string) => createElement("div", null, marker);

beforeEach(() => {
  useBottomContentStore.setState({ content: {} });
});

test("registering content stores it under the module's id", () => {
  const node = panel("codegraph panel");
  useBottomContentStore.getState().setContent("codegraph", node);
  expect(useBottomContentStore.getState().content.codegraph).toBe(node);
});

test("re-registering the same node does not produce a new state object", () => {
  // Load-bearing, not an optimisation: a module registers from an effect whose
  // dependency is the node itself. If an identical registration still produced
  // a new `content` object, SidebarSurface's equivalent subscriber would
  // re-render, the module would re-render, and the effect would fire again —
  // the "Maximum update depth exceeded" loop stores/sidebarContent.ts
  // documents.
  const node = panel("codegraph panel");
  const setContent = useBottomContentStore.getState().setContent;
  setContent("codegraph", node);
  const first = useBottomContentStore.getState().content;
  setContent("codegraph", node);
  expect(useBottomContentStore.getState().content).toBe(first);
});

test("registering null removes the module's entry", () => {
  const setContent = useBottomContentStore.getState().setContent;
  setContent("codegraph", panel("x"));
  setContent("codegraph", null);
  expect("codegraph" in useBottomContentStore.getState().content).toBe(false);
});

test("removing an entry that was never registered changes nothing", () => {
  const before = useBottomContentStore.getState().content;
  useBottomContentStore.getState().setContent("notes", null);
  expect(useBottomContentStore.getState().content).toBe(before);
});

test("two modules can be registered at once", () => {
  // Modules stay mounted-but-hidden after being visited (WorkbenchViews' mount
  // policy), so a background module must not clobber the active one's panel.
  const setContent = useBottomContentStore.getState().setContent;
  const a = panel("a");
  const b = panel("b");
  setContent("codegraph", a);
  setContent("ssh", b);
  expect(useBottomContentStore.getState().content.codegraph).toBe(a);
  expect(useBottomContentStore.getState().content.ssh).toBe(b);
});
