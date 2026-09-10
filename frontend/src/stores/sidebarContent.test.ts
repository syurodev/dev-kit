import { beforeEach, expect, test } from "bun:test";
import { useSidebarContentStore } from "./sidebarContent";

beforeEach(() => {
  useSidebarContentStore.setState({ content: {}, registrars: {} });
});

// A module with several documents open has several of them registering the
// same rail. Closing one Database tab used to blank the rail under the tabs
// still on screen, because the cleanup cleared the module's one slot outright.
test("the rail survives one of several registrars leaving", () => {
  const { setContent } = useSidebarContentStore.getState();
  const rail = "RAIL";

  setContent("database", rail, "doc-a");
  setContent("database", rail, "doc-b");
  setContent("database", null, "doc-a");

  expect(useSidebarContentStore.getState().content["database"]).toBe(rail);
});

test("the rail goes when the last registrar leaves", () => {
  const { setContent } = useSidebarContentStore.getState();

  setContent("database", "RAIL", "doc-a");
  setContent("database", "RAIL", "doc-b");
  setContent("database", null, "doc-a");
  setContent("database", null, "doc-b");

  expect(useSidebarContentStore.getState().content["database"]).toBeUndefined();
  expect(useSidebarContentStore.getState().registrars["database"]).toBeUndefined();
});

// One registrar per module stays the common case, and must behave as before.
test("a single registrar still clears on its own", () => {
  const { setContent } = useSidebarContentStore.getState();

  setContent("notes", "RAIL", "only");
  expect(useSidebarContentStore.getState().content["notes"]).toBe("RAIL");
  setContent("notes", null, "only");
  expect(useSidebarContentStore.getState().content["notes"]).toBeUndefined();
});

// An unknown leaver must not tear down a rail it never owned.
test("an unregistered leaver changes nothing", () => {
  const { setContent } = useSidebarContentStore.getState();

  setContent("database", "RAIL", "doc-a");
  setContent("database", null, "never-registered");

  expect(useSidebarContentStore.getState().content["database"]).toBe("RAIL");
});

// A Suspense skeleton registers its own placeholder rail while a lazy document
// loads. When it leaves, the rail must go back to the real one a live document
// offered — not stay on the skeleton's, which is what left a placeholder rail
// on screen after the module had finished loading.
test("a temporary registrar leaving restores the real rail", () => {
  const { setContent } = useSidebarContentStore.getState();

  setContent("database", "REAL", "document");
  setContent("database", "SKELETON", "suspense");
  expect(useSidebarContentStore.getState().content["database"]).toBe("SKELETON");

  setContent("database", null, "suspense");
  expect(useSidebarContentStore.getState().content["database"]).toBe("REAL");
});

// The skeleton's own rail is for a COLD module, where the column beside a
// loading document would otherwise be blank. On a warm one the real rail is
// already there, registered by a sibling document still on screen, and it has
// nothing to do with which chunk is loading — replacing it made opening a
// second kind of document look like the whole module reloading.
test("a skeleton that declines the rail leaves the real one alone", () => {
  const { setContent } = useSidebarContentStore.getState();

  setContent("database", "REAL", "document");
  // A skeleton that saw an existing rail registers nothing at all.
  setContent("database", null, "skeleton");

  expect(useSidebarContentStore.getState().content["database"]).toBe("REAL");
  expect(useSidebarContentStore.getState().registrars["database"]).toHaveLength(1);
});
