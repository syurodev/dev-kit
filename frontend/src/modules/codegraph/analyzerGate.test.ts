import { describe, expect, it } from "bun:test";

import type { CodeGraphToolchainStatus } from "@/bridge/codegraph";
import { blockingAnalyzers } from "./analyzerGate";

const status = (language: string, ready: boolean): CodeGraphToolchainStatus =>
  ({
    language,
    label: language,
    ready,
    hasArtifact: ready,
    buildable: true,
    artifact: "",
    bundled: false,
    buildCommand: "",
    requirements: [],
  }) as CodeGraphToolchainStatus;

describe("which missing analyzer takes over the canvas", () => {
  const javaMissing = [status("java", false), status("lua", true)];

  it("does not block a Lua project on a missing JDK", () => {
    expect(blockingAnalyzers(javaMissing, "/p", "lua")).toEqual([]);
  });

  it("blocks a Java project on a missing JDK", () => {
    expect(blockingAnalyzers(javaMissing, "/p", "java").map((s) => s.language)).toEqual(["java"]);
  });

  it("lets the user open something when one analyzer works", () => {
    expect(blockingAnalyzers(javaMissing, "", "")).toEqual([]);
  });

  it("takes over when nothing at all is ready and nothing is open", () => {
    const none = [status("java", false), status("lua", false)];
    expect(blockingAnalyzers(none, "", "").map((s) => s.language)).toEqual(["java", "lua"]);
  });

  it("blocks nothing while the open project's language is still unknown", () => {
    expect(blockingAnalyzers(javaMissing, "/p", "")).toEqual([]);
  });

  it("treats an unanswered probe as ready", () => {
    expect(blockingAnalyzers(null, "/p", "java")).toEqual([]);
  });

  it("stays out of the way when every analyzer is ready", () => {
    expect(blockingAnalyzers([status("java", true), status("lua", true)], "/p", "java")).toEqual([]);
  });
});
