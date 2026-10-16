import { useState } from "react";
import type { AgentSession } from "@/bridge/agenthub";
import { MessageSentCard } from "@/modules/agenthub/toolViews/message";
import { parseSentMessage } from "@/modules/agenthub/toolViews/messageResult";
import { PeerMessageView } from "@/modules/agenthub/transcript/PeerMessage";
import { AgentSessionSidebar } from "@/modules/agenthub/sidebar/AgentSessionSidebar";
import type { MessageSource } from "@/modules/agenthub/types";

/**
 * Messages between sessions, as the three places they show: the receiving
 * session's transcript (a label, never the person's bubble), the sending
 * session's tool result (what became of it), and the rail (a count on the row,
 * not in Needs you). Fed fixtures, no bridge. `?gallery#agenthub-peer-messages`.
 */
const NOW = Date.now();

const alpha: MessageSource = { kind: "session", sessionId: "alpha", agentType: "claude", name: "Refactor invoice rounding", messageId: "m1", hop: 1 };
const reply: MessageSource = { kind: "session", sessionId: "beta", agentType: "codex", name: "Review the OpenAPI diff", messageId: "m2", replyTo: "m1", hop: 3 };
const terminal: MessageSource = { kind: "terminal", messageId: "m3", hop: 1 };
const gone: MessageSource = { kind: "session", sessionId: "deleted", agentType: "claude", name: "Scratch idea", messageId: "m4", hop: 2 };

const LONG = Array.from(
  { length: 14 },
  (_, i) => `${i + 1}. The rounding moved to InvoiceLine.total(); callers in billing-api no longer need to round again.`,
).join("\n");

function session(id: string, title: string, over: Partial<AgentSession> = {}): AgentSession {
  return {
    id,
    agentType: "claude",
    projectPath: "/work/billing/api",
    workspaceId: "ws-billing",
    status: "idle",
    parentSessionId: null,
    depth: 0,
    title,
    acpSessionId: id,
    updatedAt: NOW - 60_000,
    ...over,
  };
}

const rail: AgentSession[] = [
  session("alpha", "Refactor invoice rounding", { updatedAt: NOW - 30_000 }),
  session("beta", "Review the OpenAPI diff", { agentType: "codex", status: "running", updatedAt: NOW - 10_000 }),
  session("gamma", "Why does the retry test flake on CI?", { updatedAt: NOW - 300_000 }),
  session("perm", "Wants to run `rm -rf build`", { status: "awaiting_permission", updatedAt: NOW - 5_000 }),
];

const sent = (value: Record<string, unknown>) => parseSentMessage(value)!;

export function AgentPeerMessagesSection() {
  const [opened, setOpened] = useState("");
  const sender = (title: string, agentType: string) => ({ title, agentType });
  return (
    <section id="agenthub-peer-messages" className="flex flex-col gap-6 p-6">
      <h2 className="text-title-16 text-text-primary">Agent Hub — messages between sessions</h2>
      <div className="flex flex-wrap items-start gap-8">
        <div className="flex w-130 flex-col gap-4" data-testid="peer-transcript">
          <h3 className="text-label-12 text-text-tertiary">In the receiving session's transcript</h3>
          <PeerMessageView source={alpha} sender={sender("Refactor invoice rounding", "claude")} onOpenSession={setOpened} at={new Date(NOW).toISOString()} text="The schema changed: use v2 of the ledger table. The migration is in db/migrate/0042." />
          <PeerMessageView source={reply} sender={sender("Review the OpenAPI diff", "codex")} onOpenSession={setOpened} text="Done: three endpoints still return the old shape. I listed them in docs/openapi-gaps.md." />
          <PeerMessageView source={terminal} text="Run the linter before you commit." />
          <PeerMessageView source={gone} text="Last note from a session that has since been deleted." />
          <PeerMessageView source={alpha} sender={sender("Refactor invoice rounding", "claude")} onOpenSession={setOpened} text={LONG} />
          <p className="text-caption-11 text-text-tertiary" data-testid="peer-opened">{opened ? `Opened session: ${opened}` : "Click a sender to open its session."}</p>
        </div>

        <div className="flex w-90 flex-col gap-3" data-testid="peer-sent">
          <h3 className="text-label-12 text-text-tertiary">The sender's tool result</h3>
          <MessageSentCard message={sent({ messageId: "a", delivery: "started", sessionId: "gamma", title: "Why does the retry test flake on CI?", hop: 1 })} onOpenSession={setOpened} />
          <MessageSentCard message={sent({ messageId: "b", delivery: "steered", sessionId: "beta", title: "Review the OpenAPI diff", hop: 2 })} onOpenSession={setOpened} />
          <MessageSentCard
            message={sent({ messageId: "c", delivery: "queued", sessionId: "perm", title: "Wants to run rm -rf build", reason: "The session is waiting for the person to answer it." })}
            onOpenSession={setOpened}
          />
          <MessageSentCard message={sent({ messageId: "d", delivery: "refused", code: "loop", reason: "This exchange is 6 messages deep (the limit is 6). Stop replying, summarise for the person, or wait for them to continue." })} />
          <MessageSentCard message={sent({ messageId: "e", delivery: "refused", code: "not_found", reason: "No session with that id is reachable from yours. agenthub.listPeers lists the sessions of your workspace." })} />
        </div>

        <div className="h-100 w-70 overflow-hidden rounded-control border border-border-subtle bg-(--background-app)" data-testid="peer-rail">
          <h3 className="px-3 pt-2 text-label-12 text-text-tertiary">The rail: a count, not a request</h3>
          <AgentSessionSidebar
            sessions={rail}
            workspaces={[{ id: "ws-billing", name: "billing-platform", paths: ["/work/billing/api"] }]}
            activeSessionId="alpha"
            attention={{ perm: { permissions: 1, questions: 0 } }}
            peerUnread={{ beta: 2, gamma: 1, perm: 12 }}
            currentWorkspaceId="ws-billing"
            onSelectSession={() => {}}
            onNewSession={() => {}}
          />
        </div>
      </div>
    </section>
  );
}
