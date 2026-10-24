import { describe, expect, it } from "bun:test";
import type { PendingPermission } from "@/bridge/agenthub";
import { selectToastPermission, shouldCollapseToastPermission } from "./permissionToastModel";

const permission = (id: string, sessionId: string, createdAt: string): PendingPermission => ({
  id, sessionId, createdAt, expiresAt: "", caller: "agent:test", title: id, depth: 0, params: {},
});

describe("permission toast selection", () => {
  it("skips the inline request and selects the oldest request from another session", () => {
    const inline = permission("inline", "visible", "2026-10-25T10:00:00Z");
    const newer = permission("newer", "other-b", "2026-10-25T10:02:00Z");
    const older = permission("older", "other-a", "2026-10-25T10:01:00Z");
    expect(selectToastPermission([inline, newer, older], inline, (id) => id === "visible")?.id).toBe("older");
  });

  it("collapses the other request while an inline permission card is present", () => {
    const items = [permission("inline", "visible", "2026-10-25T10:00:00Z"), permission("other", "elsewhere", "2026-10-25T10:01:00Z")];
    expect(shouldCollapseToastPermission(items, (id) => id === "visible")).toBe(true);
  });
});
