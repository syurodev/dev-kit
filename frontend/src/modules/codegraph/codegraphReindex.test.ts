import { beforeEach, describe, expect, it, mock } from "bun:test";
import { AppError } from "@/bridge/errors";

// Spread the real module and override only what a failed open touches —
// codegraphIntelProvider.test.ts's own comment explains why: mock.module
// replaces @/bridge/codegraph for the whole test run, and a partial
// replacement would break every other file that imports from it.
const bridge = await import("@/bridge/codegraph");

const notConfigured = new AppError({
  code: "connection_failed",
  message: "CodeGraph analyzer not configured.",
  recoverable: true,
});
const noActiveProject = new AppError({
  code: "validation_failed",
  message: "No active CodeGraph project.",
  recoverable: true,
});

// Swapped per test: the module is mocked once for the whole run.
let openProject: typeof bridge.openCodeGraphProject = async () => {
  throw notConfigured;
};
// Mirrors Go with no active project: the open above never got far enough
// to make one, so anything reindex's finally block asks for fails too.
let indexInfo: typeof bridge.codeGraphIndexInfo = async () => {
  throw noActiveProject;
};
let listFiles: typeof bridge.listCodeGraphFiles = async () => {
  throw noActiveProject;
};
let listEndpoints: typeof bridge.listCodeGraphEndpoints = async () => {
  throw noActiveProject;
};

mock.module("@/bridge/codegraph", () => ({
  ...bridge,
  openCodeGraphProject: (path: string) => openProject(path),
  codeGraphIndexInfo: () => indexInfo(),
  listCodeGraphFiles: () => listFiles(),
  listCodeGraphEndpoints: () => listEndpoints(),
  codeGraphGitStatus: async () => {
    throw noActiveProject;
  },
}));

const { reindex } = await import("./codegraphActions");
const { useCodeGraphStore } = await import("./codegraphStore");

beforeEach(() => {
  useCodeGraphStore.setState({
    projectPath: "",
    status: "",
    error: "",
    errorCode: "",
    busy: false,
    indexInfo: null,
  });
});

describe("reindex, a project whose analyzer is not configured", () => {
  it("clears indexInfo.indexing on failure, not just busy", async () => {
    await reindex("/home/dev/nestjs-app");

    const state = useCodeGraphStore.getState();
    expect(state.busy).toBe(false);
    expect(state.error).toBe("CodeGraph analyzer not configured.");
    expect(state.errorCode).toBe("connection_failed");
    // This is the regression: indexInfo.indexing was set true before the
    // open, and used to stay true forever on a failed open — Project
    // Header's status line reads it, not busy/error, so it kept rendering
    // "Indexing…" no matter what the rest of the store said.
    expect(state.indexInfo?.indexing).toBe(false);
    expect(state.indexInfo?.error).toBe("CodeGraph analyzer not configured.");
  });
});

describe("reindex, a folder no analyzer reads", () => {
  it("opens it for its files, labelled, with nothing reported as an error", async () => {
    let askedForEndpoints = false;
    openProject = async () => ({ language: "", label: "", modules: [], unsupported: true });
    listFiles = async () => [{ path: "main.tf", isDir: false }];
    listEndpoints = async () => {
      askedForEndpoints = true;
      return [];
    };
    indexInfo = async () => ({
      indexedAt: 0,
      indexing: false,
      error: "",
      modules: [],
      language: "",
      branch: "main",
      unsupported: true,
    });
    useCodeGraphStore.setState({ activeTab: "endpoints" });

    await reindex("/home/dev/tera");
    // The sidebar meta is read in reindex's finally, without being awaited.
    await new Promise((resolve) => setTimeout(resolve, 0));

    const state = useCodeGraphStore.getState();
    expect(state.error).toBe("");
    expect(state.errorCode).toBe("");
    expect(state.busy).toBe(false);
    expect(state.status).toBe("");
    expect(state.files.map((f) => f.path)).toEqual(["main.tf"]);
    // Nothing to list, so nothing is asked — and the tree is where it lands.
    expect(askedForEndpoints).toBe(false);
    expect(state.activeTab).toBe("files");
    expect(state.indexInfo?.unsupported).toBe(true);
    expect(state.indexInfo?.error).toBe("");
  });
});
