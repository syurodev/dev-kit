import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { groupTurns } from "../turns";
import type { MessageSource, TranscriptItem } from "../types";
import { AgentTurn } from "../surfaces/AgentTurn";
import { PeerMessageView, peerSourceLabel } from "./PeerMessage";

const FROM: MessageSource = { kind: "session", sessionId: "s-alpha", agentType: "claude", name: "alpha (then)", messageId: "m1", hop: 1 };

describe("a message from another session", () => {
  const alive = { title: "alpha (now)", agentType: "claude" };

  it("names the sender by its live title and opens it", () => {
    const html = renderToString(<PeerMessageView source={FROM} text="please look at the parser" sender={alive} onOpenSession={() => {}} />);
    expect(html).toContain('data-testid="peer-message"');
    expect(html).toContain("From alpha (now)");
    expect(html).not.toContain("alpha (then)");
    // A button: the label opens the session that sent it.
    expect(html).toMatch(/<button[^>]*data-testid="peer-message-source"/);
    expect(html).toContain("please look at the parser");
    expect(html).not.toContain("step ");
  });

  it("keeps the name it was sent under when the sender is gone, and says so", () => {
    const html = renderToString(<PeerMessageView source={FROM} text="hi" onOpenSession={() => {}} />);
    expect(html).toContain("From alpha (then)");
    expect(html).toContain("(session gone)");
    expect(html).not.toMatch(/<button[^>]*data-testid="peer-message-source"/);
  });

  it("says which step of an exchange it is, from the second on", () => {
    const html = renderToString(<PeerMessageView source={{ ...FROM, hop: 3 }} text="and again" sender={alive} />);
    expect(html).toContain("step 3");
  });

  it("introduces the terminal as the person's CLI, not as a session", () => {
    const source: MessageSource = { kind: "terminal", hop: 1 };
    expect(peerSourceLabel(source)).toBe("From a CLI in your terminal");
    const html = renderToString(<PeerMessageView source={source} text="from the shell" />);
    expect(html).toContain('data-source-kind="terminal"');
    expect(html).not.toContain("session gone");
  });

  it("folds a long message behind Show more", () => {
    const html = renderToString(<PeerMessageView source={FROM} text={"line\n".repeat(30)} />);
    expect(html).toContain("line-clamp-8");
    expect(html).toContain("Show more");
  });

  it("takes the place of the person's bubble in a turn, and offers nothing that edits it", () => {
    const items: TranscriptItem[] = [
      { id: "u", kind: "user_message_chunk", messageId: "m1", text: "do the thing", source: FROM, createdAt: "2026-09-14T10:00:00Z" },
      { id: "a", kind: "agent_message_chunk", text: "On it." },
    ];
    const [turn] = groupTurns(items);
    const html = renderToString(<AgentTurn turn={turn} onRewind={() => {}} onEditPrompt={() => {}} />);
    expect(html).toContain('data-testid="peer-message"');
    expect(html).not.toContain('data-testid="turn-prompt"');
    expect(html).toContain("On it.");
    // Rewind and edit act on what the person said. The same turn with the
    // person's words does offer them (the control), so their absence is the rule.
    const mine = groupTurns([{ id: "u", kind: "user_message_chunk", messageId: "m1", text: "do the thing" }]);
    const own = renderToString(<AgentTurn turn={mine[0]} onRewind={() => {}} onEditPrompt={() => {}} />);
    expect(own).toContain("Rewind");
    expect(html).not.toContain("Rewind");
    expect(html).not.toContain("Edit");
  });

  it("still draws the person's own prompt as the bubble", () => {
    const [turn] = groupTurns([{ id: "u", kind: "user_message_chunk", text: "mine" }]);
    const html = renderToString(<AgentTurn turn={turn} />);
    expect(html).toContain('data-testid="turn-prompt"');
    expect(html).not.toContain("peer-message");
  });
});
