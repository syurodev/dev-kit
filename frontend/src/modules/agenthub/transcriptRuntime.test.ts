import { describe, expect, mock, test } from "bun:test";

type Page = { entries: Record<string, unknown>[]; state: Record<string, unknown>[]; hasMore: boolean };
type PageCall = { sessionId: string; before: number; resolve: (page: Page) => void };

// Every page the runtime asks for waits here until the test answers it, so the
// order pages land in is the test's to choose. The rest of the bridge is the
// real module; only the Wails calls this file drives are replaced.
const calls: PageCall[] = [];
// The shell's one live-update listener, so a test can stream tokens into it.
let deliver: (event: { sessionId: string; parsed: unknown }) => void = () => {};
const real = await import("../../bridge/agenthub");
mock.module("../../bridge/agenthub", () => ({
  ...real,
  agentHubTranscriptPage: (sessionId: string, before: number) =>
    new Promise<Page>((resolve) => calls.push({ sessionId, before, resolve })),
  onAgentHubSessionUpdate: (handler: typeof deliver) => {
    deliver = handler;
    return () => {};
  },
}));

const { ensureTranscript, loadOlderTranscript, reloadTranscript, updateTranscriptControls, TRANSCRIPT_CACHE_LIMIT } =
  await import("./transcript");
const { useAgentSessionsStore } = await import("../../stores/agentSessions");

const entry = (seq: number) => ({
  ID: `e${seq}`,
  SessionID: "",
  Seq: seq,
  Kind: "agent_message_chunk",
  ToolCallID: "",
  Text: `row ${seq}`,
  Payload: { messageId: `m${seq}` },
  CreatedAt: "2026-09-15T00:00:00Z",
  UpdatedAt: "2026-09-15T00:00:00Z",
});
const page = (seqs: number[], hasMore = true): Page => ({ entries: seqs.map(entry), state: [], hasMore });
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const lastCall = () => calls[calls.length - 1];
const seqsOf = (id: string) =>
  (useAgentSessionsStore.getState().transcriptsById[id]?.items ?? []).map((item) => item.seq);
const session = (id: string) => ({
  id,
  agentType: "claude",
  projectPath: "/p",
  status: "idle" as const,
  parentSessionId: null,
  depth: 0,
  title: id,
  acpSessionId: "",
});
const held = (id: string) => updateTranscriptControls(id, (model) => model) !== null;

const token = (index: number) => ({
  kind: "agent_message_chunk",
  messageId: "live",
  content: { type: "text", text: `token-${index} ` },
});
// Past the frame scheduler's 50ms fallback: bun has no requestAnimationFrame.
const nextFrame = () => new Promise((resolve) => setTimeout(resolve, 60));

async function opened(id: string) {
  ensureTranscript(id);
  lastCall().resolve(page([1]));
  await settle();
}

describe("the shell stream through the real store", () => {
  test("publishes a burst of 1,000 tokens once", async () => {
    useAgentSessionsStore.getState().setSessions([session("burst")]);
    await opened("burst");
    let publishes = 0;
    const unsubscribe = useAgentSessionsStore.subscribe((state, previous) => {
      if (state.transcriptsById.burst !== previous.transcriptsById.burst) publishes += 1;
    });

    for (let index = 0; index < 1_000; index += 1) deliver({ sessionId: "burst", parsed: token(index) });
    await nextFrame();
    unsubscribe();

    expect(publishes).toBe(1);
    const items = useAgentSessionsStore.getState().transcriptsById.burst.items;
    expect(items[items.length - 1].text).toContain("token-999");
  });

  test("leaves one session's model untouched while another streams", async () => {
    useAgentSessionsStore.getState().setSessions([session("quiet"), session("busy")]);
    await opened("quiet");
    await opened("busy");
    const quiet = useAgentSessionsStore.getState().transcriptsById.quiet;
    const busy = useAgentSessionsStore.getState().transcriptsById.busy;

    deliver({ sessionId: "busy", parsed: token(0) });
    await nextFrame();

    const state = useAgentSessionsStore.getState();
    expect(state.transcriptsById.busy).not.toBe(busy);
    expect(state.transcriptsById.quiet).toBe(quiet);
  });
});

describe("a reload racing an older page", () => {
  test("drops the older page asked for before the reload", async () => {
    useAgentSessionsStore.getState().setSessions([session("race")]);
    ensureTranscript("race");
    lastCall().resolve(page([101, 102]));
    await settle();
    expect(seqsOf("race")).toEqual([101, 102]);

    loadOlderTranscript("race");
    const older = lastCall();
    expect(older.before).toBe(101);

    reloadTranscript("race");
    lastCall().resolve(page([201, 202]));
    await settle();
    older.resolve(page([99, 100]));
    await settle();

    expect(seqsOf("race")).toEqual([201, 202]);
    // The reloaded window walks back from its own edge, without waiting on the stale request.
    loadOlderTranscript("race");
    expect(lastCall().before).toBe(201);
  });
});

describe("transcripts the app no longer needs", () => {
  test("go with a session removed from the store", async () => {
    useAgentSessionsStore.getState().setSessions([session("gone")]);
    ensureTranscript("gone");
    lastCall().resolve(page([1]));
    await settle();
    expect(held("gone")).toBe(true);

    useAgentSessionsStore.getState().removeSession("gone");

    expect(held("gone")).toBe(false);
    expect(useAgentSessionsStore.getState().transcriptsById.gone).toBeUndefined();
  });

  test("go when the session list comes back without them", async () => {
    useAgentSessionsStore.getState().setSessions([session("pruned"), session("kept")]);
    ensureTranscript("pruned");
    lastCall().resolve(page([1]));
    await settle();

    useAgentSessionsStore.getState().setSessions([session("kept")]);

    expect(held("pruned")).toBe(false);
    expect(useAgentSessionsStore.getState().transcriptsById.pruned).toBeUndefined();
  });

  test("are kept only for the most recently opened sessions", async () => {
    const ids = Array.from({ length: TRANSCRIPT_CACHE_LIMIT + 1 }, (_, index) => `lru-${index}`);
    useAgentSessionsStore.getState().setSessions(ids.map(session));
    for (const id of ids) {
      ensureTranscript(id);
      lastCall().resolve(page([1]));
      await settle();
    }

    expect(held(ids[0])).toBe(false);
    expect(useAgentSessionsStore.getState().transcriptsById[ids[0]]).toBeUndefined();
    expect(held(ids[1])).toBe(true);
    expect(held(ids[ids.length - 1])).toBe(true);
  });
});
