import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentAccount } from "@/bridge/agentAccounts";
import { agentAccountsKey } from "../accounts/useAgentAccounts";
import { AgentTurn } from "../surfaces/AgentTurn";
import { groupTurns } from "../turns";
import type { AccountNotice as AccountNoticeData, TranscriptItem } from "../types";
import { AccountNotice, AccountNoticeLine } from "./AccountNotice";

const account = (id: string, label: string, builtin = false): AgentAccount => ({
  id,
  vendor: "claude",
  label,
  builtin,
  signedIn: true,
  shared: false,
  exhaustedUntil: 0,
  cooldownUntil: 0,
  usage: [],
  usageSource: "",
  observedAt: 0,
});

const move: AccountNoticeData = { kind: "switch", from: "claude-default", to: "claude-work", reason: "5-hour 95% ≥ 90%" };

function withAccounts(node: React.ReactNode, accounts?: AgentAccount[]) {
  const client = new QueryClient();
  if (accounts) client.setQueryData(agentAccountsKey("claude"), accounts);
  return renderToString(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

describe("AccountNotice", () => {
  it("a move reads as a quiet line with the reason after a dot", () => {
    const html = renderToString(<AccountNotice notice={move} fromLabel="System default" toLabel="Work" />);
    expect(html).toContain('data-testid="account-notice"');
    expect(html).toContain("Switched from System default to Work · 5-hour 95% ≥ 90%");
    expect(html).toContain("text-text-tertiary");
    expect(html).not.toContain("text-status-warning");
  });

  it("a move that did not happen leads with that, in the warning tone", () => {
    const html = renderToString(
      <AccountNotice notice={{ kind: "failed", from: "a", to: "b", reason: "It could not be opened." }} fromLabel="Work" toLabel="Spare" />,
    );
    expect(html).toContain("Could not switch from Work to Spare: It could not be opened.");
    expect(html).toContain("text-status-warning");
    expect(html).toContain('data-kind="failed"');
  });
});

describe("AccountNoticeLine", () => {
  it("names the accounts from the list", () => {
    const html = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />, [
      account("claude-default", "System default", true),
      account("claude-work", "Work"),
    ]);
    expect(html).toContain("Switched from System default to Work");
  });

  it("falls back for the built-in account, a removed one and a list not read yet", () => {
    const removed = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />, [account("claude-default", "System default", true)]);
    expect(removed).toContain("Switched from System default to a removed account");
    const waiting = withAccounts(<AccountNoticeLine notice={move} vendor="claude" />);
    expect(waiting).toContain("Switched from System default to another account");
  });
});

describe("in the transcript", () => {
  const items: TranscriptItem[] = [
    { id: "u1", kind: "user_message_chunk", text: "first", messageId: "m1", createdAt: "2026-10-23T08:00:00Z" },
    { id: "a1", kind: "agent_message_chunk", text: "Done.", messageId: "a1", createdAt: "2026-10-23T08:00:01Z" },
    { id: "n1", kind: "devkit_account_notice", accountNotice: move, createdAt: "2026-10-23T08:00:02Z" },
    { id: "u2", kind: "user_message_chunk", text: "second", messageId: "m2", createdAt: "2026-10-23T08:00:03Z" },
  ];

  it("is drawn at the end of the turn it follows and nowhere else", () => {
    const turns = groupTurns(items);
    const first = withAccounts(<AgentTurn turn={turns[0]} vendor="claude" />, [account("claude-default", "System default", true), account("claude-work", "Work")]);
    expect(first).toContain("Switched from System default to Work");
    expect(first.indexOf("Done.")).toBeLessThan(first.indexOf("Switched from"));
    const second = withAccounts(<AgentTurn turn={turns[1]} vendor="claude" />);
    expect(second).not.toContain("Switched from");
  });

  it("a turn that is only a notice (the first turn moved before it began) still draws it", () => {
    const [turn] = groupTurns([{ id: "n0", kind: "devkit_account_notice", accountNotice: move }]);
    expect(withAccounts(<AgentTurn turn={turn} vendor="claude" />)).toContain("Switched from");
  });

  it("other notes stay undrawn", () => {
    const [turn] = groupTurns([{ id: "x", kind: "something_unmodelled" }]);
    expect(withAccounts(<AgentTurn turn={turn} vendor="claude" />)).not.toContain("account-notice");
  });
});
