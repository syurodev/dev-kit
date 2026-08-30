import { describe, expect, it } from "bun:test";
import { severityOf, statusLabel, type SystemStatus } from "./SystemStatusMenu";

const clear: SystemStatus = { vault: "unlocked", conflicts: 0, agentsRunning: 0 };

describe("severityOf", () => {
  it("is neutral when nothing is wrong", () => {
    expect(severityOf(clear)).toBe("neutral");
  });

  // A dropdown hides detail; the icon must not also hide that there is any.
  it("raises attention for a locked vault and for conflicts", () => {
    expect(severityOf({ ...clear, vault: "locked" })).toBe("attention");
    expect(severityOf({ ...clear, conflicts: 1 })).toBe("attention");
  });

  // Running is information, not a problem — it must not borrow the colour that
  // means "something is blocking you".
  it("keeps a running agent below a blocking state", () => {
    expect(severityOf({ ...clear, agentsRunning: 2 })).toBe("active");
    expect(severityOf({ ...clear, agentsRunning: 2, conflicts: 1 })).toBe("attention");
    expect(severityOf({ ...clear, agentsRunning: 2, vault: "locked" })).toBe("attention");
  });
});

describe("statusLabel", () => {
  // Colour and a badge are the only visual signals on an icon-only titlebar,
  // and a screen reader gets neither.
  it("says every live condition in words", () => {
    expect(statusLabel({ vault: "locked", conflicts: 2, agentsRunning: 1 })).toBe(
      "System status: vault locked, 2 sync conflicts, 1 agent running",
    );
  });

  it("says so when there is nothing to report", () => {
    expect(statusLabel(clear)).toBe("System status: all clear");
  });

  it("counts in the singular where it should", () => {
    expect(statusLabel({ ...clear, conflicts: 1 })).toBe("System status: 1 sync conflict");
  });
});
