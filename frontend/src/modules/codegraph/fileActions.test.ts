import { beforeEach, describe, expect, it, mock } from "bun:test";
import { AppError } from "@/bridge/errors";

/**
 * The file actions against a fake disk: what the bridge would do, kept in a Map
 * so the tests can change "the disk" between a read and a save.
 *
 * The real module is spread so that only the calls under test are replaced
 * (bun's mock.module applies to the whole run).
 */
// The tab store persists through the bridge on a timer; with no desktop runtime that
// is an unhandled rejection that lands in an unrelated test file.
mock.module("@/bridge/uistate", () => {
  const mem = new Map<string, unknown>();
  return {
    getUIState: async (key: string) => String(mem.get(key) ?? ""),
    setUIState: async (key: string, value: string) => void mem.set(key, value),
    uiStateStorage: {
      getItem: (key: string) => mem.get(key) ?? null,
      setItem: (key: string, value: unknown) => void mem.set(key, value),
      removeItem: (key: string) => void mem.delete(key),
    },
  };
});

const real = await import("@/bridge/workspaceCode");

type Disk = Map<string, string>;
const disk: Disk = new Map();
const writes: Array<{ workspaceId: string; folderId: string; path: string; content: string; expected: string }> = [];
let removedFolders = new Set<string>();

const rev = (s: string) => `r:${s.length}:${s}`;
const diskKey = (folderId: string, path: string) => `${folderId}:${path}`;

function appError(code: string, message = code) {
  return new AppError({ code, message, recoverable: false, details: undefined } as never);
}

mock.module("@/bridge/workspaceCode", () => ({
  ...real,
  readFolderFile: async (_w: string, folderId: string, path: string) => {
    if (removedFolders.has(folderId)) throw appError("folder_not_in_workspace");
    const content = disk.get(diskKey(folderId, path));
    if (content === undefined) throw appError("not_found");
    return { content, revision: rev(content), size: content.length, modifiedAt: 1 };
  },
  statFolderFile: async (_w: string, folderId: string, path: string) => {
    if (removedFolders.has(folderId)) throw appError("folder_not_in_workspace");
    const content = disk.get(diskKey(folderId, path));
    return content === undefined
      ? { exists: false, revision: "", size: 0, modifiedAt: 0, unreadable: "" }
      : { exists: true, revision: rev(content), size: content.length, modifiedAt: 1, unreadable: "" };
  },
  writeFolderFile: async (workspaceId: string, folderId: string, path: string, content: string, expected: string) => {
    writes.push({ workspaceId, folderId, path, content, expected });
    if (removedFolders.has(folderId)) throw appError("folder_not_in_workspace");
    const current = disk.get(diskKey(folderId, path));
    const actual = current === undefined ? "absent" : rev(current);
    if (expected !== "*" && expected !== actual) throw appError("file_conflict");
    disk.set(diskKey(folderId, path), content);
    return { revision: rev(content), modifiedAt: 2 };
  },
  watchDocuments: async () => [],
  folderFileOutline: async () => ({ symbols: [], supported: false, indexedAt: 0, stale: false }),
  listFolderFiles: async () => ({ entries: [], truncated: false }),
  listFolderStates: async () => [],
  folderGitStatus: async () => ({}),
  folderEntryPoints: async () => ({ state: "unsupported", entries: [], indexedAt: 0, stale: false, error: "", missingToolchain: false }),
}));

const { useCodeGraphStore } = await import("./codegraphStore");
const actions = await import("./fileActions");
const { docKey } = await import("./workspace/identity");
const { focusedDocument } = await import("./documents");
const { useNavigationStore } = await import("./navigation");


const WS = "w1";
const folder = (id: string, path: string) => ({ id, workspace_id: WS, name: id, path, name_source: "" });

function reset(folderIds = ["fa", "fb"]) {
  disk.clear();
  writes.length = 0;
  removedFolders = new Set();
  useNavigationStore.setState({ histories: {} });
  useCodeGraphStore.setState({
    workspaceId: WS,
    folders: folderIds.map((id) => folder(id, `/ws/${id}`)),
    primaryId: folderIds[0],
    buffers: {},
    data: {},
  } as never);
}

const key = (folderId: string, path = "src/config.ts") => docKey(WS, folderId, path);
const buffer = (k: string) => useCodeGraphStore.getState().buffers[k];

beforeEach(() => reset());

describe("file actions over a workspace", () => {
  it("keeps the same relative path in two folders as two independent buffers", async () => {
    disk.set("fa:src/config.ts", "export const a = 1\n");
    disk.set("fb:src/config.ts", "export const b = 2\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "src/config.ts" });
    await actions.openFile({ workspaceId: WS, folderId: "fb", path: "src/config.ts" });

    actions.editFile(key("fa"), "export const a = 10\n");
    expect(buffer(key("fa")).dirty).toBe(true);
    expect(buffer(key("fb")).dirty).toBe(false);
    expect(buffer(key("fb")).content).toBe("export const b = 2\n");

    await actions.saveFile(key("fa"));
    expect(writes).toHaveLength(1);
    expect(writes[0].folderId).toBe("fa");
    expect(disk.get("fa:src/config.ts")).toBe("export const a = 10\n");
    expect(disk.get("fb:src/config.ts")).toBe("export const b = 2\n");
  });

  it("saves into the buffer's folder whichever folder is primary", async () => {
    disk.set("fb:src/config.ts", "old\n");
    await actions.openFile({ workspaceId: WS, folderId: "fb", path: "src/config.ts" });
    actions.editFile(key("fb"), "new\n");

    // The primary moves while the file is being edited.
    useCodeGraphStore.setState({ primaryId: "fb" } as never);
    useCodeGraphStore.setState({ primaryId: "fa" } as never);
    await actions.saveFile(key("fb"));

    expect(writes.map((w) => w.folderId)).toEqual(["fb"]);
    expect(buffer(key("fb")).dirty).toBe(false);
  });

  it("does not overwrite a file that changed on disk under unsaved edits", async () => {
    disk.set("fa:src/config.ts", "base\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "src/config.ts" });
    actions.editFile(key("fa"), "mine\n");

    disk.set("fa:src/config.ts", "theirs\n");
    await actions.reconcileFile(key("fa"));
    expect(buffer(key("fa")).external).toBe("changed");
    expect(buffer(key("fa")).content).toBe("mine\n");

    // "Keep mine" quiets the banner; a plain Save is still refused, nothing is written.
    actions.keepMine(key("fa"));
    await actions.saveFile(key("fa"));
    expect(writes).toHaveLength(0);
    expect(disk.get("fa:src/config.ts")).toBe("theirs\n");

    // Overwriting is a separate, explicit act.
    await actions.saveFile(key("fa"), "overwrite");
    expect(disk.get("fa:src/config.ts")).toBe("mine\n");
  });

  it("follows the disk when the buffer is clean", async () => {
    disk.set("fa:a.ts", "one\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });
    disk.set("fa:a.ts", "two\n");
    await actions.reconcileFile(key("fa", "a.ts"));
    expect(buffer(key("fa", "a.ts")).content).toBe("two\n");
    expect(buffer(key("fa", "a.ts")).external).toBeNull();
  });

  it("turns a save refused by the disk into a conflict and keeps the edits", async () => {
    disk.set("fa:a.ts", "base\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });
    actions.editFile(key("fa", "a.ts"), "mine\n");
    // Changed between the last look and the save: the write itself is refused.
    disk.set("fa:a.ts", "sneaky\n");
    await actions.saveFile(key("fa", "a.ts"));
    const b = buffer(key("fa", "a.ts"));
    expect(b.content).toBe("mine\n");
    expect(b.external).toBe("changed");
    expect(disk.get("fa:a.ts")).toBe("sneaky\n");
  });

  it("keeps a draft whose folder left the workspace, and refuses to save it", async () => {
    disk.set("fb:a.ts", "base\n");
    await actions.openFile({ workspaceId: WS, folderId: "fb", path: "a.ts" });
    actions.editFile(key("fb", "a.ts"), "unsaved\n");
    disk.set("fa:a.ts", "clean\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });

    // fb is removed from the workspace.
    removedFolders.add("fb");
    useCodeGraphStore.setState({ folders: [folder("fa", "/ws/fa")], primaryId: "fa" } as never);
    actions.markRemovedFolders();

    const draft = buffer(key("fb", "a.ts"));
    expect(draft.content).toBe("unsaved\n");
    expect(draft.orphan).toBe("folder_removed");
    await actions.saveFile(key("fb", "a.ts"));
    expect(writes.filter((w) => w.folderId === "fb")).toHaveLength(0);

    // The same path added back is a new folder id: it never adopts the old draft.
    useCodeGraphStore.setState({ folders: [folder("fa", "/ws/fa"), folder("fb2", "/ws/fb")] } as never);
    expect(buffer(key("fb2", "a.ts"))).toBeUndefined();
    // The clean buffer of a surviving folder is untouched.
    expect(buffer(key("fa", "a.ts")).content).toBe("clean\n");
  });

  it("drops the buffer of a removed folder when it has no edits", async () => {
    disk.set("fb:a.ts", "x\n");
    await actions.openFile({ workspaceId: WS, folderId: "fb", path: "a.ts" });
    useCodeGraphStore.setState({ folders: [folder("fa", "/ws/fa")] } as never);
    actions.markRemovedFolders();
    expect(buffer(key("fb", "a.ts"))).toBeUndefined();
  });

  it("saves a buffer to its own workspace after the title bar switched workspace", async () => {
    disk.set("fa:a.ts", "x\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });
    actions.editFile(key("fa", "a.ts"), "edited\n");

    useCodeGraphStore.setState({ workspaceId: "w2", folders: [folder("zz", "/other")] } as never);
    // The draft survived the switch and still names workspace w1.
    expect(buffer(key("fa", "a.ts")).dirty).toBe(true);
    await actions.saveFile(key("fa", "a.ts"));
    expect(writes[0].workspaceId).toBe(WS);
    expect(writes[0].folderId).toBe("fa");
  });

  it("does not mark a buffer clean when the person typed while the save was in flight", async () => {
    disk.set("fa:a.ts", "x\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });
    actions.editFile(key("fa", "a.ts"), "first\n");
    const saving = actions.saveFile(key("fa", "a.ts"));
    actions.editFile(key("fa", "a.ts"), "first and more\n");
    await saving;
    const b = buffer(key("fa", "a.ts"));
    expect(b.content).toBe("first and more\n");
    expect(b.dirty).toBe(true);
  });

  it("keeps a deleted file's draft instead of re-creating it", async () => {
    disk.set("fa:a.ts", "x\n");
    await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" });
    actions.editFile(key("fa", "a.ts"), "mine\n");
    disk.delete("fa:a.ts");
    await actions.reconcileFile(key("fa", "a.ts"));
    expect(buffer(key("fa", "a.ts")).external).toBe("deleted");
    await actions.saveFile(key("fa", "a.ts"));
    expect(disk.has("fa:a.ts")).toBe(false);
    await actions.saveFile(key("fa", "a.ts"), "recreate");
    expect(disk.get("fa:a.ts")).toBe("mine\n");
  });

  describe("Back / Forward across folders", () => {
    const frontPlace = () => {
      const doc = focusedDocument()?.doc;
      return doc?.kind === "file" ? `${doc.folderId}:${doc.path}` : "";
    };

    it("walks back and forward through files of different folders, and cuts the branch on a new jump", async () => {
      disk.set("fa:a.ts", "a\n");
      disk.set("fb:a.ts", "b\n");
      disk.set("fb:c.ts", "c\n");
      disk.set("fa:d.ts", "d\n");
      await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" }, 1, { preview: false });
      await actions.openFile({ workspaceId: WS, folderId: "fb", path: "a.ts" }, 1, { preview: false });
      await actions.openFile({ workspaceId: WS, folderId: "fb", path: "c.ts" }, 1, { preview: false });

      expect(await actions.goBack()).toBe(true);
      expect(frontPlace()).toBe("fb:a.ts");
      expect(await actions.goBack()).toBe(true);
      expect(frontPlace()).toBe("fa:a.ts");
      expect(await actions.goForward()).toBe(true);
      expect(frontPlace()).toBe("fb:a.ts");

      // A new, deliberate jump after Back drops what was ahead.
      await actions.openFile({ workspaceId: WS, folderId: "fa", path: "d.ts" }, 1, { preview: false });
      expect(await actions.goForward()).toBe(false);
      expect(await actions.goBack()).toBe(true);
      expect(frontPlace()).toBe("fb:a.ts");
    });

    it("skips a place whose file was deleted instead of showing an error", async () => {
      disk.set("fa:a.ts", "a\n");
      disk.set("fa:gone.ts", "g\n");
      disk.set("fb:z.ts", "z\n");
      await actions.openFile({ workspaceId: WS, folderId: "fa", path: "a.ts" }, 1, { preview: false });
      await actions.openFile({ workspaceId: WS, folderId: "fa", path: "gone.ts" }, 1, { preview: false });
      await actions.openFile({ workspaceId: WS, folderId: "fb", path: "z.ts" }, 1, { preview: false });
      disk.delete("fa:gone.ts");

      expect(await actions.goBack()).toBe(true);
      expect(frontPlace()).toBe("fa:a.ts");
    });
  });
});
