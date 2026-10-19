import { describe, expect, it } from "bun:test";

import { drainQueuedPrompt, queuedPrompt, resumeQueuedPrompts, sendAgentTurn, type TurnBridge } from "./agentPromptQueue";
import { handleAgentTurnFinished } from "./AgentSessionsSubscriber";
import { useAgentSessionsStore, type QueuedAgentPrompt } from "../stores/agentSessions";
import type { AgentSession, InvokeResult, InvokeSessionInput } from "../bridge/agenthub";
import { AppError } from "../bridge/errors";

const queued = (id: string): QueuedAgentPrompt => ({ id, text: id, attachments: [], images: [], refs: [] });
const completed: InvokeResult = { status: "completed", summary: "", filesChanged: [], turnCount: 1, stopReason: "end_turn" };
const session = (id: string, status: AgentSession["status"] = "idle"): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/p",
  status,
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: "",
});
const busy = () => new AppError({ code: "validation_failed", message: "Agent session is busy.", recoverable: true });
const cancelled = () => new AppError({ code: "operation_cancelled", message: "Operation cancelled.", recoverable: true });
const crashed = () => new AppError({ code: "connection_failed", message: "Agent process failed.", recoverable: true });
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

function reset(...sessions: AgentSession[]) {
  useAgentSessionsStore.setState({
    sessionsById: Object.fromEntries(sessions.map((s) => [s.id, s])),
    sessionIds: sessions.map((s) => s.id),
    queuedPromptsBySession: {},
    turnInFlightBySession: {},
    queuePausedBySession: {},
    queuePauseReasonBySession: {},
    pending: null,
    pendingQueue: [],
    activeSessionId: "",
    chatOpen: false,
    unreadCompletedBySession: {},
  });
}

const store = () => useAgentSessionsStore.getState();
const queue = (id = "s1") => (store().queuedPromptsBySession[id] ?? []).map((prompt) => prompt.id);
const status = (id = "s1") => store().sessionsById[id]?.status;

/** A backend whose turns end when the test says so. */
function backend() {
  const sent: InvokeSessionInput[] = [];
  const turns: Array<{ resolve: (result: InvokeResult) => void; reject: (error: unknown) => void }> = [];
  let after: AgentSession["status"] = "idle";
  const bridge: TurnBridge = {
    invoke: (input) => {
      sent.push(input);
      return new Promise((resolve, reject) => turns.push({ resolve, reject }));
    },
    getStatus: async (id) => ({ ...session(id), status: after }),
  };
  return {
    bridge,
    sent: () => sent.map((input) => input.prompt),
    turns,
    statusAfter: (next: AgentSession["status"]) => {
      after = next;
    },
  };
}

describe("one send path for every turn", () => {
  it("marks the session running while a queued prompt is sent", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);

    expect(b.sent()).toEqual(["first"]);
    expect(status()).toBe("running");
    b.turns[0].resolve(completed);
    await settle();
    expect(status()).toBe("idle");
  });

  it("leaves the session idle when the user denied a permission, and says why the queue paused", async () => {
    // Denying is an answer, not a crash. It used to end the turn as an error,
    // which put a red "stopped … Restart" card under a session that was fine.
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    b.turns[0].resolve({ ...completed, status: "denied" });
    await settle();

    expect(status()).toBe("idle");
    expect(store().queuePausedBySession.s1).toBe(true);
    expect(store().queuePauseReasonBySession.s1).toBe("You denied a permission.");
    // The prompts behind it wait for Resume rather than running on.
    expect(queue()).toEqual(["second"]);
    expect(b.sent()).toEqual(["first"]);
  });

  it("takes a prompt out of the queue as soon as its turn starts", () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));

    void drainQueuedPrompt("s1", backend().bridge);

    expect(queue()).toEqual(["second"]);
  });

  it("never resends a prompt whose turn was cancelled", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    b.turns[0].reject(cancelled());
    await settle();
    expect(queue()).toEqual([]);

    const manual = sendAgentTurn("s1", queued("manual"), b.bridge);
    b.turns[1].resolve(completed);
    await manual;
    await settle();
    expect(b.sent()).toEqual(["first", "manual"]);
  });

  it("puts a prompt back at the head when the backend refused it before the turn began", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    b.turns[0].reject(busy());
    await settle();

    expect(queue()).toEqual(["first", "second"]);
    expect(store().queuePausedBySession.s1).toBeFalsy();
  });

  it("keeps draining when turnFinished lands before the invoke returns", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    // Go emits turnFinished from Invoke's defer, before the RPC returns.
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    await settle();
    expect(b.sent()).toEqual(["first"]);
    expect(status()).toBe("running");

    b.turns[0].resolve(completed);
    await settle();
    expect(b.sent()).toEqual(["first", "second"]);

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    b.turns[1].resolve(completed);
    await settle();
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    await settle();
    expect(b.sent()).toEqual(["first", "second"]);
  });

  it("pauses the queue after a failed turn, even past a later successful one, until Resume", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    b.statusAfter("error");
    b.turns[0].reject(crashed());
    await settle();
    expect(store().queuePausedBySession.s1).toBe(true);
    expect(queue()).toEqual(["second"]);
    expect(status()).toBe("error");

    const manual = sendAgentTurn("s1", queued("manual"), b.bridge);
    b.turns[1].resolve(completed);
    await manual;
    await settle();
    // A manual send while paused does not resume the queue.
    expect(b.sent()).toEqual(["first", "manual"]);
    expect(store().queuePausedBySession.s1).toBe(true);

    resumeQueuedPrompts("s1", b.bridge);
    expect(store().queuePausedBySession.s1).toBeFalsy();
    expect(b.sent()).toEqual(["first", "manual", "second"]);
  });

  it("pauses the queue when a manual turn is cancelled", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("waiting"));
    const b = backend();

    const manual = sendAgentTurn("s1", queued("manual"), b.bridge);
    b.turns[0].reject(cancelled());
    await expect(manual).rejects.toThrow("Operation cancelled.");
    await settle();

    expect(store().queuePausedBySession.s1).toBe(true);
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    await settle();
    expect(b.sent()).toEqual(["manual"]);
  });

  it("keeps the text of a manual send the backend refused as busy", async () => {
    reset(session("s1"));
    const b = backend();

    b.statusAfter("running");
    const manual = sendAgentTurn("s1", { id: "typed", text: "hello", attachments: [], images: [], refs: [] }, b.bridge);
    b.turns[0].reject(busy());
    expect(await manual).toBeNull();
    await settle();
    expect(queue()).toEqual(["typed"]);
    expect(store().queuePausedBySession.s1).toBeFalsy();

    // The turn that held the session ends; the refused prompt goes next.
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    await settle();
    expect(b.sent()).toEqual(["hello", "hello"]);
  });

  it("sends a busy-refused prompt when the turn that held the session finished before the refusal arrived", async () => {
    // Another agent's turn holds the session; this window still shows it idle.
    reset(session("s1"));
    const b = backend();

    const manual = sendAgentTurn("s1", queued("typed"), b.bridge);
    // That turn's turnFinished reaches the webview before the RPC's rejection.
    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    b.statusAfter("idle");
    b.turns[0].reject(busy());
    expect(await manual).toBeNull();
    await settle();

    expect(b.sent()).toEqual(["typed", "typed"]);
    expect(queue()).toEqual([]);
    b.turns[1].resolve(completed);
    await settle();
    expect(b.sent()).toEqual(["typed", "typed"]);
  });

  it("does not retry a busy refusal until a turn is seen to finish", async () => {
    // Go also refuses while another invoke is still resuming, before its status says running.
    reset(session("s1"));
    const b = backend();

    const manual = sendAgentTurn("s1", queued("typed"), b.bridge);
    b.turns[0].reject(busy());
    await manual;
    await settle();
    expect(b.sent()).toEqual(["typed"]);
    expect(queue()).toEqual(["typed"]);

    handleAgentTurnFinished({ sessionId: "s1", status: "idle", outcome: "completed" }, b.bridge);
    expect(b.sent()).toEqual(["typed", "typed"]);
  });

  it("does not pause the queue when a manual send is refused before its turn began", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("waiting"));
    const b = backend();

    const manual = sendAgentTurn("s1", queued("manual"), b.bridge);
    b.turns[0].reject(new AppError({ code: "validation_failed", message: "Image is too large.", recoverable: true }));
    await expect(manual).rejects.toThrow("Image is too large.");
    await settle();

    expect(store().queuePausedBySession.s1).toBeFalsy();
    // No turn ran; the session is idle, so what was waiting goes.
    expect(b.sent()).toEqual(["manual", "waiting"]);
  });

  it("says why a refused queued prompt paused the queue, until Resume", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    b.turns[0].reject(new AppError({ code: "validation_failed", message: "Attachment not found.", recoverable: true }));
    await settle();
    expect(store().queuePausedBySession.s1).toBe(true);
    expect(store().queuePauseReasonBySession.s1).toBe("Attachment not found.");

    resumeQueuedPrompts("s1", b.bridge);
    expect(store().queuePauseReasonBySession.s1).toBeFalsy();
    expect(b.sent()).toEqual(["first", "first"]);
  });

  it("clears the pause once the last queued prompt is removed", () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().pauseQueue("s1");
    expect(store().queuePausedBySession.s1).toBe(true);

    store().removeQueuedPrompt("s1", "first");

    expect(store().queuePausedBySession.s1).toBeFalsy();
  });
});

describe("the queue only sends where it may", () => {
  it("sends a session's own prompts and nobody else's", async () => {
    reset(session("s1"), session("s2"));
    store().enqueuePrompt("s1", queued("first"));
    const b = backend();

    expect(await drainQueuedPrompt("s2", b.bridge)).toBe(false);
    expect(b.sent()).toEqual([]);

    void drainQueuedPrompt("s1", b.bridge);
    expect(b.sent()).toEqual(["first"]);
  });

  it("does not send to a deleted session", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().removeSession("s1");
    const b = backend();

    expect(await drainQueuedPrompt("s1", b.bridge)).toBe(false);
    expect(b.sent()).toEqual([]);
  });

  it("does not send to a closed or still-running session", async () => {
    for (const current of ["closed", "running", "awaiting_permission"] as const) {
      reset(session("s1", current));
      store().enqueuePrompt("s1", queued("first"));
      const b = backend();

      expect(await drainQueuedPrompt("s1", b.bridge)).toBe(false);
      expect(b.sent()).toEqual([]);
      expect(queue()).toEqual(["first"]);
    }
  });

  it("does not send while the session has a permission request pending", async () => {
    reset(session("s1"));
    store().enqueuePending({
      id: "permission-1",
      sessionId: "s1",
      params: {},
      caller: "agent:claude",
      title: "Run command",
      depth: 0,
      createdAt: "",
      expiresAt: "",
    });
    store().enqueuePrompt("s1", queued("first"));
    const b = backend();

    expect(await drainQueuedPrompt("s1", b.bridge)).toBe(false);
    expect(b.sent()).toEqual([]);
    expect(queue()).toEqual(["first"]);
  });

  it("leaves a turn in flight alone when a waiting prompt is cancelled", async () => {
    reset(session("s1"));
    store().enqueuePrompt("s1", queued("first"));
    store().enqueuePrompt("s1", queued("second"));
    const b = backend();

    void drainQueuedPrompt("s1", b.bridge);
    store().removeQueuedPrompt("s1", "second");
    b.turns[0].resolve(completed);
    await settle();

    expect(b.sent()).toEqual(["first"]);
    expect(store().turnInFlightBySession.s1).toBeFalsy();
  });
});

describe("queuedPrompt", () => {
  it("remembers the draft it was sent from, so editing it brings the chips back", () => {
    const queued = queuedPrompt("look at @a.md", ["/w/a.md"], [], [], "look at file:%2Fw%2Fa.md|a.md ");
    expect(queued.draft).toBe("look at file:%2Fw%2Fa.md|a.md ");
    expect(queued.attachments).toEqual(["/w/a.md"]);
  });

  it("has no draft field when there was none (a retry, /compact)", () => {
    expect("draft" in queuedPrompt("/compact", [], [])).toBe(false);
    expect("draft" in queuedPrompt("hi", [], [], [], "")).toBe(false);
  });
});
