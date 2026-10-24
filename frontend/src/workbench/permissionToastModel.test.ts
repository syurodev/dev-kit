import { describe, expect, it } from "bun:test";
import type { PendingElicitation, PendingPermission } from "@/bridge/agenthub";
import { heldToastIsLive, selectToastPermission, shouldCollapseToast } from "./permissionToastModel";

const permission = (id: string, sessionId: string, createdAt: string): PendingPermission => ({
  id, sessionId, createdAt, expiresAt: "", caller: "agent:test", title: id, depth: 0, params: {},
});
const question = (id: string, sessionId: string): PendingElicitation => ({
  id, sessionId, toolCallId: id, message: "A question", params: {}, caller: "agent:test",
  title: id, depth: 0, createdAt: "2026-10-25T10:00:00Z",
});

describe("permission toast selection", () => {
  it("skips the inline request and selects the oldest request from another session", () => {
    const inline = permission("inline", "visible", "2026-10-25T10:00:00Z");
    const newer = permission("newer", "other-b", "2026-10-25T10:02:00Z");
    const older = permission("older", "other-a", "2026-10-25T10:01:00Z");
    expect(selectToastPermission([inline, newer, older], inline, (id) => id === "visible")?.id).toBe("older");
  });

  const cases = [
    ["permission beside inline permission", [permission("ip", "visible", "")], [], [permission("tp", "elsewhere", "")], [], true],
    ["question beside inline question", [], [question("iq", "visible")], [], [question("tq", "elsewhere")], true],
    ["question beside inline permission", [permission("ip", "visible", "")], [], [], [question("tq", "elsewhere")], true],
    ["permission beside inline question", [], [question("iq", "visible")], [permission("tp", "elsewhere", "")], [], true],
    ["permission and question both inline", [permission("ip", "visible", "")], [question("iq", "visible")], [], [], true],
    ["external requests with no inline card", [], [], [permission("tp", "elsewhere", "")], [question("tq", "elsewhere")], false],
  ] as const;
  for (const [name, inlinePermissions, inlineQuestions, toastPermissions, toastQuestions, expected] of cases) {
    it(name, () => {
      const requested = [...inlinePermissions, ...inlineQuestions, ...toastPermissions, ...toastQuestions] as
        Array<PendingPermission | PendingElicitation>;
      expect(shouldCollapseToast(
        requested.filter((item): item is PendingPermission => "expiresAt" in item),
        requested.filter((item): item is PendingElicitation => !("expiresAt" in item)),
        (id) => id === "visible",
      )).toBe(expected);
    });
  }

  it("drops a held pill when its request is withdrawn or moves inline", () => {
    const held = permission("held", "other", "2026-10-25T10:00:00Z");
    expect(heldToastIsLive(held, [], [], () => false)).toBe(false);
    expect(heldToastIsLive(held, [held], [], (id) => id === "other")).toBe(false);
    expect(heldToastIsLive(held, [held], [], () => false)).toBe(true);
  });
});
