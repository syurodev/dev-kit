import { describe, expect, it } from "bun:test";

import type { AgentSession } from "@/bridge/agenthub";
import { statusAfterAnswer } from "./permissions";

const session = (status: AgentSession["status"]) => ({ id: "s1", status }) as AgentSession;

describe("statusAfterAnswer", () => {
  it("reads a status that still says waiting as running: the turn goes on", () => {
    // The read can land before Go puts the session back to running.
    expect(statusAfterAnswer(session("awaiting_permission"), false).status).toBe("running");
  });

  it("keeps waiting when the next request for the session has already arrived", () => {
    expect(statusAfterAnswer(session("awaiting_permission"), true).status).toBe("awaiting_permission");
  });

  it("leaves a turn that already ended as it ended", () => {
    for (const status of ["idle", "error", "closed", "running"] as const) {
      expect(statusAfterAnswer(session(status), false).status).toBe(status);
    }
  });
});
