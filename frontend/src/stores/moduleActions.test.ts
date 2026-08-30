import { beforeEach, describe, expect, it } from "bun:test";
import { useModuleActionsStore, type ModuleAction } from "./moduleActions";

const action = (id: string, isActive = false): ModuleAction => ({
  id,
  icon: (() => null) as unknown as ModuleAction["icon"],
  label: id,
  isActive,
  onClick: () => {},
});

const ids = () => useModuleActionsStore.getState().actions.map((a) => a.id);

describe("module action registry", () => {
  beforeEach(() => {
    useModuleActionsStore.setState({ actions: [] });
  });

  // Toggling one action must not reorder the row. useModuleAction used to
  // unregister and re-register on every isActive change, which appended the
  // action and sent it past its own siblings.
  it("keeps an action's place when it is re-registered", () => {
    const { registerAction } = useModuleActionsStore.getState();
    registerAction(action("first"));
    registerAction(action("second"));

    registerAction(action("first", true));

    expect(ids()).toEqual(["first", "second"]);
    expect(useModuleActionsStore.getState().actions[0].isActive).toBe(true);
  });

  it("appends only an action it has not seen", () => {
    const { registerAction } = useModuleActionsStore.getState();
    registerAction(action("first"));
    registerAction(action("second"));
    expect(ids()).toEqual(["first", "second"]);
  });

  // The failure this fix exists for, written down. There is no React test
  // harness in this project, so the hook itself is not covered — this pins the
  // sequence that broke it, so nobody reintroduces it by making the store
  // "smarter" instead.
  it("loses its place if an unregister precedes the re-register", () => {
    const { registerAction, unregisterAction } = useModuleActionsStore.getState();
    registerAction(action("first"));
    registerAction(action("second"));

    unregisterAction("first");
    registerAction(action("first", true));

    expect(ids()).toEqual(["second", "first"]);
  });

  it("removes an action on unregister", () => {
    const { registerAction, unregisterAction } = useModuleActionsStore.getState();
    registerAction(action("first"));
    registerAction(action("second"));
    unregisterAction("first");
    expect(ids()).toEqual(["second"]);
  });
});
