import { describe, test, expect } from "bun:test";
import type { AgentAccount } from "@/bridge/agentAccounts";
import {
  usageAccountName,
  initialWindow,
  validWindow,
  formatBytes,
} from "./measurementModel";
import { measurementOptions } from "./useUsageMeasurements";
describe("host usage measurements", () => {
  test("generation must match before a current label is used", () => {
    const accounts = [
      { id: "work", label: "New label", usageGeneration: "new" },
    ] as AgentAccount[];
    expect(usageAccountName("work", "old", accounts)).toContain(
      "removed generation",
    );
    expect(usageAccountName("work", "new", accounts)).toBe("New label");
    expect(usageAccountName("", "", accounts)).toContain("legacy");
    expect(usageAccountName("work", "old", undefined)).toContain("loading");
    expect(usageAccountName("work", "old", undefined, true)).toContain(
      "unavailable",
    );
  });
  test("date window is UTC and capped at 90 inclusive days", () => {
    expect(initialWindow(new Date("2026-10-26T23:00:00-07:00"))).toEqual({
      from: "2026-09-28",
      to: "2026-10-27",
    });
    expect(validWindow("2026-01-01", "2026-03-31")).toBe(true);
    expect(validWindow("2026-01-01", "2026-04-01")).toBe(false);
    expect(validWindow("2026-02-30", "2026-03-02")).toBe(false);
    expect(validWindow("", "2026-03-02")).toBe(false);
    expect(formatBytes(0)).toBe("0 B");
  });
  test("queries separate vendor, window, and cursor and refresh on focus", () => {
    const a = measurementOptions("claude", "2026-10-01", "2026-10-26", "");
    const b = measurementOptions("codex", "2026-10-01", "2026-10-26", "page2");
    expect(a.tools.queryKey).not.toEqual(b.tools.queryKey);
    expect(a.moves.queryKey).not.toEqual(b.moves.queryKey);
    expect(a.tools.refetchOnWindowFocus).toBe(true);
    expect(a.moves.refetchOnWindowFocus).toBe(true);
    expect(measurementOptions("claude", "bad", "bad", "").tools.enabled).toBe(
      false,
    );
  });
});
