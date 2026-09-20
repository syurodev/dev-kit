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

mock.module("@/bridge/codegraph", () => ({
  ...bridge,
  openCodeGraphProject: async () => {
    throw notConfigured;
  },
  // Mirrors Go with no active project: the open above never got far enough
  // to make one, so anything reindex's finally block asks for fails too.
  codeGraphIndexInfo: async () => {
    throw noActiveProject;
  },
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
