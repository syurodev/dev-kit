import { describe, expect, it } from "bun:test";
import type { TranscriptItem } from "../types";
import { earlierTurnItemIds, failureLine, settleToolState } from "./toolSettle";

describe("settleToolState", () => {
  const states = ["pending", "running", "awaiting", "done", "failed"] as const;

  it("lets a call of the latest turn carry on as it is while the session may still finish it", () => {
    for (const state of states) expect(settleToolState(state, { earlierTurn: false })).toBe(state);
    for (const state of states) expect(settleToolState(state, { earlierTurn: false, sessionQuiet: false })).toBe(state);
  });

  it("settles a call of the latest turn when the session is quiet: nothing is working, nothing waits", () => {
    const quiet = { earlierTurn: false, sessionQuiet: true };
    expect(settleToolState("pending", quiet)).toBe("unfinished");
    expect(settleToolState("running", quiet)).toBe("unfinished");
    expect(settleToolState("awaiting", quiet)).toBe("unfinished");
    expect(settleToolState("done", quiet)).toBe("done");
    expect(settleToolState("failed", quiet)).toBe("failed");
  });

  it("leaves a background subagent's call of the latest turn alone: the session has no signal for it", () => {
    const quietSubagent = { earlierTurn: false, sessionQuiet: true, subagent: true };
    for (const state of states) expect(settleToolState(state, quietSubagent)).toBe(state);
  });

  it("still settles a subagent's call once its turn is an earlier one", () => {
    expect(settleToolState("running", { earlierTurn: true, sessionQuiet: false, subagent: true })).toBe("unfinished");
    expect(settleToolState("running", { earlierTurn: true, sessionQuiet: true, subagent: true })).toBe("unfinished");
  });

  it("settles a call of an earlier turn that can no longer finish, and only those", () => {
    expect(settleToolState("pending", { earlierTurn: true })).toBe("unfinished");
    expect(settleToolState("running", { earlierTurn: true })).toBe("unfinished");
    expect(settleToolState("awaiting", { earlierTurn: true })).toBe("unfinished");
    // What ended stays what it was.
    expect(settleToolState("done", { earlierTurn: true })).toBe("done");
    expect(settleToolState("failed", { earlierTurn: true })).toBe("failed");
    expect(settleToolState("unfinished", { earlierTurn: true })).toBe("unfinished");
  });
});

const item = (id: string, kind: string): TranscriptItem => ({ id, kind }) as TranscriptItem;

describe("earlierTurnItemIds", () => {
  it("is every item before the newest user message", () => {
    const items = [item("u1", "user_message_chunk"), item("t1", "tool_call"), item("u2", "user_message_chunk"), item("t2", "tool_call")];
    expect([...earlierTurnItemIds(items)]).toEqual(["u1", "t1"]);
  });

  it("is empty for a transcript of one turn, or none", () => {
    expect(earlierTurnItemIds([item("u1", "user_message_chunk"), item("t1", "tool_call")]).size).toBe(0);
    expect(earlierTurnItemIds([item("t1", "tool_call")]).size).toBe(0);
    expect(earlierTurnItemIds([]).size).toBe(0);
  });
});

describe("failureLine", () => {
  it("is empty when there is nothing to say", () => {
    expect(failureLine({})).toBe("");
    expect(failureLine({ rawOutput: "" })).toBe("");
    expect(failureLine({ rawOutput: "  \n\n  " })).toBe("");
    expect(failureLine({ rawOutput: null, content: [] })).toBe("");
  });

  it("takes the first non-empty line of a text output, trimmed", () => {
    expect(failureLine({ rawOutput: "\n\n  Exit code 1  \nstack\nmore" })).toBe("Exit code 1");
  });

  it("clips a very long line", () => {
    const line = failureLine({ rawOutput: "x".repeat(500) }, 80);
    expect(line.length).toBe(80);
    expect(line.endsWith("…")).toBe(true);
    expect(failureLine({ rawOutput: "y".repeat(80) }, 80)).toBe("y".repeat(80));
  });

  it("reads an error out of an object output, and says nothing for a payload", () => {
    expect(failureLine({ rawOutput: { error: "permission denied\nmore" } })).toBe("permission denied");
    expect(failureLine({ rawOutput: { message: "no such file" } })).toBe("no such file");
    expect(failureLine({ rawOutput: { result: [1, 2, 3] } })).toBe("");
    expect(failureLine({ rawOutput: { error: { code: 5 } } })).toBe("");
    expect(failureLine({ rawOutput: '{"result":[{"id":"caad65a3"}]}' })).toBe("");
    expect(failureLine({ rawOutput: "[1,2]" })).toBe("");
  });

  it("falls back to the text content a vendor sends instead of rawOutput", () => {
    const content = [
      { type: "diff" as const, path: "/a" },
      { type: "content" as const, content: { type: "text", text: "\nExit code 2\nboom" } },
    ];
    expect(failureLine({ content })).toBe("Exit code 2");
    expect(failureLine({ rawOutput: "from raw", content })).toBe("from raw");
  });

  it("is plain text: terminal colours are dropped", () => {
    expect(failureLine({ rawOutput: "\u001b[31merror:\u001b[0m bad thing" })).toBe("error: bad thing");
  });
});
