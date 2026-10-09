import { beforeEach, describe, expect, it, mock } from "bun:test";

const real = await import("@/bridge/workspace");
const realCode = await import("@/bridge/workspaceCode");

type Gate = { promise: Promise<void>; open: () => void };
const gate = (): Gate => {
  let open = () => {};
  const promise = new Promise<void>((resolve) => (open = resolve));
  return { promise, open };
};

const gates: Record<string, Gate> = {};
const service = (workspaceId: string, id: string, path: string) => ({ id, workspace_id: workspaceId, name: id, path, name_source: "" });
const workspace = (id: string, root: string) => ({ id, name: id, root }) as never;

mock.module("@/bridge/workspace", () => ({
  ...real,
  listWorkspaces: async () => [workspace("w1", "/one/a"), workspace("w2", "/two/x")],
  listWorkspaceServices: async (id: string) => {
    await gates[id]?.promise;
    return id === "w1" ? [service("w1", "a", "/one/a"), service("w1", "b", "/one/b")] : [service("w2", "x", "/two/x")];
  },
  workspaceIndexStatus: async () => [],
}));
mock.module("@/bridge/workspaceCode", () => ({
  ...realCode,
  listFolderStates: async () => [],
  listFolderFiles: async () => ({ entries: [], truncated: false }),
  folderGitStatus: async () => ({}),
  folderEntryPoints: async () => ({ state: "unsupported", entries: [], indexedAt: 0, stale: false, error: "", missingToolchain: false }),
}));

const { useCodeGraphStore } = await import("./codegraphStore");
const { useWorkspaceSwitcherStore } = await import("@/stores/workspaceSwitcher");
const sync = await import("./workspaceSync");

const state = () => useCodeGraphStore.getState();

beforeEach(() => {
  for (const k of Object.keys(gates)) delete gates[k];
  useCodeGraphStore.setState({ workspaceId: "", folders: [], foldersStatus: "idle", buffers: {}, data: {} } as never);
});

describe("workspace sync", () => {
  it("reads the primary from the workspace root, never from folder order", async () => {
    useWorkspaceSwitcherStore.setState({ currentId: "w1" } as never);
    await sync.syncWorkspace();
    expect(state().folders.map((f) => f.id)).toEqual(["a", "b"]);
    expect(state().primaryId).toBe("a");
  });

  it("drops the answer of a workspace the person has already left", async () => {
    gates.w1 = gate();
    useWorkspaceSwitcherStore.setState({ currentId: "w1" } as never);
    const slow = sync.syncWorkspace();

    useWorkspaceSwitcherStore.setState({ currentId: "w2" } as never);
    await sync.syncWorkspace();
    expect(state().workspaceId).toBe("w2");
    expect(state().folders.map((f) => f.id)).toEqual(["x"]);

    // The first workspace's folders arrive late: they must not land in w2's context.
    gates.w1.open();
    await slow;
    expect(state().workspaceId).toBe("w2");
    expect(state().folders.map((f) => f.id)).toEqual(["x"]);
  });

  it("keeps unsaved buffers of the old workspace when the workspace changes", async () => {
    useWorkspaceSwitcherStore.setState({ currentId: "w1" } as never);
    await sync.syncWorkspace();
    useCodeGraphStore.setState({
      buffers: { "w1:a:src/a.ts": { workspaceId: "w1", folderId: "a", path: "src/a.ts", content: "draft", dirty: true } },
    } as never);
    useWorkspaceSwitcherStore.setState({ currentId: "w2" } as never);
    await sync.syncWorkspace();
    expect(Object.keys(state().buffers)).toHaveLength(1);
  });

  it("changing the discovery scope keeps documents, buffers and the primary", async () => {
    useWorkspaceSwitcherStore.setState({ currentId: "w1" } as never);
    await sync.syncWorkspace();
    useCodeGraphStore.setState({ buffers: { k: { dirty: true } } } as never);
    sync.setScope({ kind: "folders", ids: ["b"] });
    expect(state().scope).toEqual({ kind: "folders", ids: ["b"] });
    expect(state().primaryId).toBe("a");
    expect(Object.keys(state().buffers)).toEqual(["k"]);
  });
});
