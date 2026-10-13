import { describe, expect, it } from "bun:test";
import type { FolderEntryPoints, FolderState } from "@/bridge/workspaceCode";
import type { CodeGraphEntryPoint } from "@/bridge/codegraph";
import type { ServiceIndexStatus } from "@/bridge/workspace";
import { batchProgress, folderHealth, summarizeHealth } from "./indexHealth";

const NOW = 1_700_000_000_000;
const fs = (over: Partial<FolderState> = {}): FolderState => ({ folderId: "f", accessible: true, error: "", isRepo: true, branch: "main", changed: 0, ...over });
const entry: CodeGraphEntryPoint = { id: "http", entryNode: "handler", verb: "GET", target: "/", kind: "http", owner: "Controller", group: "Controller", service: "", order: 0 };
const entries = (over: Partial<FolderEntryPoints> = {}): FolderEntryPoints => ({ state: "indexed", entries: [entry], indexedAt: NOW - 120_000, stale: false, error: "", missingToolchain: false, ...over });
const status = (over: Partial<ServiceIndexStatus> = {}): ServiceIndexStatus => ({ path: "/p", state: "indexed", error: "", missingToolchain: false, indexedAt: NOW, ...over });

describe("folderHealth", () => {
  it("zero entries distinguish completed indexing from unsupported discovery", () => {
    const h = folderHealth(fs(), entries({ entries: [], entryPointSupport: "Spring HTTP and Camel Java routes only." }), status(), NOW);
    expect(h.health).toBe("limited");
    expect(h.tone).toBe("warn");
    expect(h.label).toContain("Indexed");
    expect(h.detail).toContain("Spring HTTP and Camel");
    expect(summarizeHealth([h]).tone).toBe("warn");
  });
  it("an unresolved handler remains visible even when some routes were found", () => {
    const h = folderHealth(fs(), entries({ entryPointWarnings: ["Camel route handler is unresolved: fanout"] }), status(), NOW);
    expect(h.health).toBe("limited");
    expect(h.detail).toContain("handler is unresolved");
  });
  it("legacy indexes do not claim framework coverage", () => {
    expect(folderHealth(fs(), entries({ entries: [] }), status(), NOW).detail).toContain("support was not reported");
  });
  it("a folder that cannot be read is unavailable whatever its index says", () => {
    expect(folderHealth(fs({ accessible: false, error: "folder not found" }), entries(), status(), NOW).health).toBe("unavailable");
  });

  it("a fresh index is ready and says how old it is", () => {
    const h = folderHealth(fs(), entries(), status(), NOW);
    expect(h.health).toBe("ready");
    expect(h.detail).toContain("2m ago");
  });

  it("indexing keeps the old index available and says so", () => {
    const h = folderHealth(fs(), entries(), status({ state: "indexing" }), NOW);
    expect(h.health).toBe("indexing");
    expect(h.detail).toContain("stays available");
  });

  it("serving the stored index during a refresh is stale, not ready", () => {
    expect(folderHealth(fs(), entries({ stale: true }), undefined, NOW).health).toBe("stale");
  });

  it("separates a missing analyzer from a failure of the code", () => {
    expect(folderHealth(fs(), entries({ state: "failed" }), status({ state: "failed", missingToolchain: true, error: "no JDK" }), NOW).health).toBe("needs-analyzer");
    expect(folderHealth(fs(), entries({ state: "failed" }), status({ state: "failed", error: "parse error" }), NOW).health).toBe("failed");
  });

  it("an unsupported folder is a state, not an error", () => {
    const h = folderHealth(fs(), entries({ state: "unsupported" }), status({ state: "unsupported" }), NOW);
    expect(h.health).toBe("unsupported");
    expect(h.tone).toBe("neutral");
  });

  it("says nothing rather than guess before anything has answered", () => {
    expect(folderHealth(undefined, undefined, undefined, NOW).label).toBe("Checking");
  });
});

describe("summarizeHealth", () => {
  it("adds folders up without mixing their states", () => {
    const s = summarizeHealth([
      folderHealth(fs(), entries(), status(), NOW),
      folderHealth(fs(), entries(), status(), NOW),
      folderHealth(fs(), entries(), status({ state: "indexing" }), NOW),
      folderHealth(fs(), entries({ state: "failed" }), status({ state: "failed" }), NOW),
    ]);
    expect(s.text).toBe("2 ready · 1 indexing · 1 failed");
    expect(s.tone).toBe("failed");
    expect(s.total).toBe(4);
  });

  it("is neutral when nothing is ready and nothing is wrong", () => {
    expect(summarizeHealth([folderHealth(fs(), entries({ state: "not-indexed" }), undefined, NOW)]).tone).toBe("neutral");
  });
});

describe("batchProgress", () => {
  it("has a denominator only while a batch runs", () => {
    expect(batchProgress([status(), status()])).toBeNull();
    expect(batchProgress([status(), status({ state: "indexing" }), status({ state: "pending" })])).toEqual({ done: 1, total: 3 });
  });
});
