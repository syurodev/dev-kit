import { describe, expect, it } from "bun:test";
import { AppError } from "@/bridge/errors";
import { isNoPlanError, isProjectChangedError } from "./codegraphErrors";

const err = (code: string, message: string) => new AppError({ code, message, recoverable: true });

describe("isProjectChangedError", () => {
  it("matches the bridge's project_changed code", () => {
    expect(isProjectChangedError(err("project_changed", "The open CodeGraph project changed."))).toBe(true);
    expect(isProjectChangedError(err("io", "disk full"))).toBe(false);
  });
});

describe("isNoPlanError", () => {
  it("separates the normal no-plan answer from real failures", () => {
    expect(isNoPlanError(err("validation_failed", "No active plan."))).toBe(true);
    expect(isNoPlanError(err("internal", "database is locked"))).toBe(false);
  });
});
