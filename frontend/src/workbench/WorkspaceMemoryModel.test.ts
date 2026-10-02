import { describe, expect, test } from "bun:test";
import {
  emptyMemory,
  LatestRequest,
  memoryBrowseInput,
  isMemoryConflict,
  memoryDraft,
  nextCursors,
  sameDraft,
  sourceLabel,
  validateMemory,
} from "./WorkspaceMemoryModel";

describe("workspace memory controller helpers", () => {
  test("superseded responses and responses after unmount are ignored", () => {
    const requests = new LatestRequest();
    const first = requests.begin();
    const second = requests.begin();
    expect(first()).toBe(false);
    expect(second()).toBe(true);
    requests.invalidate();
    expect(second()).toBe(false);
  });
  test("request families remain independent", () => {
    const list = new LatestRequest();
    const detail = new LatestRequest();
    const current = list.begin();
    detail.begin();
    expect(current()).toBe(true);
  });
  test("cursor history retains only page positions and allows previous", () => {
    const first = [""];
    const second = nextCursors(first, "opaque-2");
    const third = nextCursors(second, "opaque-3");
    expect(first).toEqual([""]);
    expect(third.slice(0, -1)).toEqual(second);
    expect(nextCursors(third, "")).toBe(third);
  });
  test("dirty comparison includes all editable fields and ignores server metadata", () => {
    const input = {
      ...emptyMemory(),
      title: "title",
      summary: "summary",
      body: "body",
    };
    const draft = memoryDraft({ ...input, revision: 2 } as typeof input);
    expect(sameDraft(draft, input)).toBe(true);
    for (const field of ["kind", "title", "summary", "body"] as const)
      expect(sameDraft({ ...draft, [field]: "changed" }, input)).toBe(false);
    expect(Object.keys(draft)).toEqual(["kind", "title", "summary", "body"]);
  });
  test("validation measures UTF-8 bytes instead of UTF-16 characters", () => {
    const input = {
      ...emptyMemory(),
      title: "é".repeat(128),
      summary: "summary",
      body: "body",
    };
    expect(validateMemory(input)).toBe("");
    expect(validateMemory({ ...input, title: input.title + "é" })).toContain(
      "256",
    );
    expect(validateMemory({ ...input, body: " " })).toContain("required");
    expect(validateMemory({ ...input, summary: "🙂".repeat(257) })).toContain(
      "1,024",
    );
  });
  test("attribution shows vendor and session when present", () => {
    expect(
      sourceLabel({ caller: "agent", agentType: "codex", sessionId: "abc" }),
    ).toBe("codex · session abc");
    expect(sourceLabel({ caller: "ui", agentType: "", sessionId: "" })).toBe(
      "ui",
    );
  });
});

test("browse requests keep scope filters and cursor while bounding every page to 20", () => {
  expect(
    memoryBrowseInput("auth decision", "decision", true, "opaque cursor"),
  ).toEqual({
    query: "auth decision",
    kind: "decision",
    deleted: true,
    cursor: "opaque cursor",
    limit: 20,
  });
});
test("revision conflicts use the stable bridge code even when error wording changes", () => {
  expect(
    isMemoryConflict({
      code: "memory.revisionConflict",
      message: "Changed elsewhere",
    }),
  ).toBe(true);
  expect(isMemoryConflict({ code: "memory.notFound" })).toBe(false);
  expect(isMemoryConflict(null)).toBe(false);
});
