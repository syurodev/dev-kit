import { describe, expect, it } from "bun:test";

import { isNewSessionShortcut, renameAgentSession, retryTurnFor, type TurnInput } from "./AgentHubView";
import { submitRename } from "./surfaces/RenameSessionDialog";
import { useAgentSessionsStore } from "../../stores/agentSessions";
import { AppError } from "../../bridge/errors";

const key = (overrides: Partial<Parameters<typeof isNewSessionShortcut>[0]> = {}) => ({
  key: "n",
  metaKey: false,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  repeat: false,
  isComposing: false,
  defaultPrevented: false,
  ...overrides,
});

describe("the new-session shortcut", () => {
  it("is ⌘N on macOS and leaves Ctrl+N to the text field", () => {
    expect(isNewSessionShortcut(key({ metaKey: true }), true, false)).toBe(true);
    expect(isNewSessionShortcut(key({ ctrlKey: true }), true, false)).toBe(false);
  });

  it("is Ctrl+N on Windows and Linux", () => {
    expect(isNewSessionShortcut(key({ ctrlKey: true }), false, false)).toBe(true);
    expect(isNewSessionShortcut(key({ metaKey: true }), false, false)).toBe(false);
  });

  it("stands aside for a handled key, IME composition, and an open dialog or menu", () => {
    expect(isNewSessionShortcut(key({ metaKey: true, defaultPrevented: true }), true, false)).toBe(false);
    expect(isNewSessionShortcut(key({ metaKey: true, isComposing: true }), true, false)).toBe(false);
    expect(isNewSessionShortcut(key({ metaKey: true }), true, true)).toBe(false);
    expect(isNewSessionShortcut(key({ metaKey: true, shiftKey: true }), true, false)).toBe(false);
  });
});

describe("renaming a session", () => {
  const session = {
    id: "s1",
    agentType: "claude",
    projectPath: "/p",
    status: "running" as const,
    parentSessionId: null,
    depth: 0,
    title: "Old name",
    acpSessionId: "",
  };

  it("keeps the dialog's reason when the rename is refused", async () => {
    useAgentSessionsStore.setState({ sessionsById: { s1: session }, sessionIds: ["s1"] });
    const refuse = async () => {
      throw new AppError({ code: "validation_failed", message: "Agent session not found.", recoverable: true });
    };

    const shown = await submitRename("New name", (title) => renameAgentSession("s1", title, refuse));

    expect(shown).toBe("Agent session not found.");
    expect(useAgentSessionsStore.getState().sessionsById.s1.title).toBe("Old name");
  });

  it("closes with nothing to say once renamed, keeping the session's live status", async () => {
    useAgentSessionsStore.setState({ sessionsById: { s1: session }, sessionIds: ["s1"] });

    const shown = await submitRename("New name", (title) => renameAgentSession("s1", title, async () => {}));

    expect(shown).toBe("");
    expect(useAgentSessionsStore.getState().sessionsById.s1).toEqual({ ...session, title: "New name" });
  });
});

// Restart on a session that failed before a relaunch did nothing: the view's
// memory of its last turn was gone, and the fallback was an empty composer.
describe("the turn Retry and Restart send again", () => {
  const sent: TurnInput = { sessionId: "s1", text: "fix the build", attachments: ["/repo/a.ts"], images: [], refs: [] };
  const items = [
    { kind: "user_message_chunk", text: "first" },
    { kind: "agent_message_chunk", text: "ok" },
    { kind: "user_message_chunk", text: "write the tests", origin: "sub-1" },
    { kind: "user_message_chunk", text: "  xin chào  " },
    { kind: "agent_message_chunk", text: "…" },
  ];

  it("is the turn this view sent, files and all, when it was for this session", () => {
    expect(retryTurnFor("s1", sent, items)).toBe(sent);
  });

  it("is the session's last recorded prompt otherwise", () => {
    expect(retryTurnFor("s2", sent, items)).toEqual({ sessionId: "s2", text: "xin chào", attachments: [], images: [], refs: [] });
    expect(retryTurnFor("s2", null, items)?.text).toBe("xin chào");
  });

  it("skips a subagent's prompt", () => {
    expect(retryTurnFor("s2", null, items.slice(0, 3))?.text).toBe("first");
  });

  it("is nothing when there is no prompt with words to resend", () => {
    expect(retryTurnFor("s2", null, [])).toBeUndefined();
    expect(retryTurnFor("s2", null, [{ kind: "user_message_chunk", text: "" }])).toBeUndefined();
    expect(retryTurnFor("", sent, items)).toBeUndefined();
  });
});
