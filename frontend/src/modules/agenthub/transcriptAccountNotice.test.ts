import { describe, expect, test } from "bun:test";
import {
  ACCOUNT_NOTICE_KIND,
  applySessionUpdate,
  createInitialTranscriptModel,
  dropRecordedChunks,
  initTranscript,
  modelFromNewestPage,
  readAccountNotice,
  sameAccountNotice,
} from "./transcript";
import { groupTurns } from "./turns";
import type { AccountNotice, SessionUpdate } from "./types";

const move: AccountNotice = { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };
const noticeUpdate = (notice: AccountNotice = move): SessionUpdate => ({
  kind: "session_info_update",
  accountNotice: notice,
  raw: { sessionUpdate: "session_info_update" },
});

describe("readAccountNotice", () => {
  test("takes a switch or a failed switch between two ids, and nothing else", () => {
    expect(readAccountNotice(move)).toEqual(move);
    expect(readAccountNotice({ kind: "failed", from: "a", to: "b" })).toEqual({ kind: "failed", from: "a", to: "b" });
    expect(readAccountNotice({ kind: "failed", from: "a", to: "b", reason: "" })).toEqual({ kind: "failed", from: "a", to: "b" });
    for (const bad of [null, undefined, "x", {}, { kind: "other", from: "a", to: "b" }, { kind: "switch", from: "a", to: "" }, { kind: "switch", from: 1, to: "b" }]) {
      expect(readAccountNotice(bad)).toBeNull();
    }
  });

  test("two notices are the same when kind, ends and reason are", () => {
    expect(sameAccountNotice(move, { ...move })).toBe(true);
    expect(sameAccountNotice(move, { ...move, reason: "other" })).toBe(false);
    expect(sameAccountNotice(move, { ...move, kind: "failed" })).toBe(false);
    expect(sameAccountNotice(move, null)).toBe(false);
  });
});

describe("a live notice", () => {
  test("becomes a row of its own and leaves the session state alone", () => {
    let model = applySessionUpdate(createInitialTranscriptModel(), { kind: "session_info_update", goal: { objective: "ship" } });
    model = applySessionUpdate(model, noticeUpdate());
    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({ kind: ACCOUNT_NOTICE_KIND, accountNotice: move });
    expect(model.goal?.objective).toBe("ship");
    expect(model.failure).toBeNull();
  });

  test("a title or a goal update draws nothing", () => {
    const model = applySessionUpdate(createInitialTranscriptModel(), { kind: "session_info_update", title: "Refund worker" });
    expect(model.items).toEqual([]);
  });

  test("a malformed notice draws nothing", () => {
    const model = applySessionUpdate(createInitialTranscriptModel(), {
      kind: "session_info_update",
      accountNotice: { kind: "switch", from: "a", to: "" },
    });
    expect(model.items).toEqual([]);
  });

  test("sits between the turns: after the answer it follows, before the next prompt", () => {
    let model = createInitialTranscriptModel();
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "first" }, messageId: "m1" });
    model = applySessionUpdate(model, { kind: "agent_message_chunk", content: { type: "text", text: "done" }, messageId: "a1" });
    model = applySessionUpdate(model, noticeUpdate());
    model = applySessionUpdate(model, { kind: "user_message_chunk", content: { type: "text", text: "second" }, messageId: "m2" });
    const turns = groupTurns(model.items);
    expect(turns).toHaveLength(2);
    expect(turns[0].notes.map((n) => n.accountNotice)).toEqual([move]);
    expect(turns[1].notes).toEqual([]);
  });
});

describe("a stored notice", () => {
  const entry = (id: string, payload: unknown) => ({
    ID: id,
    Kind: "session_info_update",
    Payload: JSON.stringify(payload),
    Seq: 7,
    CreatedAt: "2026-10-23T08:00:00Z",
    UpdatedAt: "2026-10-23T08:00:00Z",
  });

  test("comes back as the row the live update made", () => {
    const model = initTranscript([entry("r1", { kind: "session_info_update", accountNotice: move, raw: {} })], "s1");
    expect(model.items).toHaveLength(1);
    expect(model.items[0]).toMatchObject({ id: "r1", kind: ACCOUNT_NOTICE_KIND, seq: 7, sessionId: "s1", accountNotice: move });
  });

  test("a stored title row still draws nothing", () => {
    expect(initTranscript([entry("r2", { kind: "session_info_update", title: "Named" })], "s1").items).toEqual([]);
  });
});

describe("a notice that arrived while the page loaded", () => {
  const stored = (notice: AccountNotice) => ({
    ID: "r1",
    Kind: "session_info_update",
    Payload: JSON.stringify({ kind: "session_info_update", accountNotice: notice }),
    Seq: 3,
  });

  test("is not drawn twice when the page already holds it", () => {
    const model = modelFromNewestPage({ entries: [stored(move)], state: [] }, [noticeUpdate()], "s1");
    expect(model.items.filter((item) => item.accountNotice)).toHaveLength(1);
  });

  test("is drawn when the page does not hold it, or holds a different one", () => {
    const empty = modelFromNewestPage({ entries: [], state: [] }, [noticeUpdate()], "s1");
    expect(empty.items.filter((item) => item.accountNotice)).toHaveLength(1);
    const back: AccountNotice = { kind: "switch", from: "claude-work", to: "claude-default", reason: "Weekly 95% ≥ 90%" };
    const other = modelFromNewestPage({ entries: [stored(move)], state: [] }, [noticeUpdate(back)], "s1");
    expect(other.items.filter((item) => item.accountNotice).map((item) => item.accountNotice)).toEqual([move, back]);
  });

  test("only one buffered copy is dropped", () => {
    const model = initTranscript([stored(move)], "s1");
    expect(dropRecordedChunks(model, [noticeUpdate(), noticeUpdate()])).toHaveLength(1);
  });
});
