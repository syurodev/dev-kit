import { describe, expect, it } from "bun:test";
import { callCanStillFinish, failureLine, settleToolState } from "./toolSettle";

describe("settleToolState", () => {
  const states = ["pending", "running", "awaiting", "done", "failed"] as const;

  it("lets a call of a running session carry on as it is", () => {
    for (const state of states) expect(settleToolState(state, { sessionRunning: true })).toBe(state);
  });

  it("settles a call that can no longer finish, and only those", () => {
    expect(settleToolState("pending", { sessionRunning: false })).toBe("unfinished");
    expect(settleToolState("running", { sessionRunning: false })).toBe("unfinished");
    expect(settleToolState("awaiting", { sessionRunning: false })).toBe("unfinished");
    // What ended stays what it was.
    expect(settleToolState("done", { sessionRunning: false })).toBe("done");
    expect(settleToolState("failed", { sessionRunning: false })).toBe("failed");
    expect(settleToolState("unfinished", { sessionRunning: false })).toBe("unfinished");
  });
});

describe("callCanStillFinish", () => {
  it.each([
    ["running", false, true],
    ["awaiting_permission", false, true],
    // A turn of ours is in flight before the row says running.
    ["idle", true, true],
    ["error", true, true],
    ["idle", false, false],
    ["error", false, false],
    ["closed", false, false],
    // A status nobody knows is not a reason to say a call is over.
    ["", false, true],
    [undefined, false, true],
  ] as const)("status %p, our turn in flight %p -> %p", (status, inFlight, expected) => {
    expect(callCanStillFinish(status, inFlight)).toBe(expected);
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
