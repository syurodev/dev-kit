import { beforeEach, describe, expect, it } from "bun:test";
import type * as React from "react";
import { renderToString } from "react-dom/server";
import type { AgentSession } from "@/bridge/agenthub";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { activeAgentHubSessionId, closeAgentSessionTabs, openAgentSession } from "../documents";
import { useSubagentPanel } from "../subagents";
import type { Subagent } from "../types";
import { SessionActionsContext, type SessionActions } from "./actions";
import { AgentSessionCard, type AgentSessionCardProps } from "./AgentSessionCard";
import { AgentSessionSidebar, type AgentSessionSidebarProps } from "./AgentSessionSidebar";
import { AgentSubagentAvatars } from "./AgentSubagentAvatars";
import { attentionBySession } from "./model";
import { showSubagent } from "./openSubagent";

useWorkbenchTabsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });

const session = (id: string, over: Partial<AgentSession> = {}): AgentSession => ({
  id,
  agentType: "claude",
  projectPath: "/work/api",
  workspaceId: "",
  status: "idle",
  parentSessionId: null,
  depth: 0,
  title: `Title ${id}`,
  acpSessionId: "",
  updatedAt: 0,
  ...over,
});

/** n sessions in one folder, s0 the newest. */
const aged = (n: number, over: Partial<AgentSession> = {}) =>
  Array.from({ length: n }, (_, i) => session(`s${i}`, { updatedAt: 1_000_000 - i * 1000, ...over }));

const noop = () => {};
function render(over: Partial<AgentSessionSidebarProps> = {}): string {
  const node: React.ReactNode = (
    <TooltipProvider>
      <AgentSessionSidebar sessions={[]} onSelectSession={noop} onNewSession={noop} {...over} />
    </TooltipProvider>
  );
  return renderToString(node);
}

const NO_ACTIONS: SessionActions = {
  open: noop,
  openSubagent: noop,
  descendantCount: () => 0,
  hold: noop,
  release: noop,
};

/** One row on its own, for what the rail does not pass yet (an ask, a folder). */
function renderCard(over: Partial<AgentSessionCardProps> & Pick<AgentSessionCardProps, "session">): string {
  return renderToString(
    <TooltipProvider>
      <SessionActionsContext.Provider value={NO_ACTIONS}>
        <AgentSessionCard selected={false} pinned={false} subagents={[]} ask={null} folder={null} now={Date.now()} {...over} />
      </SessionActionsContext.Provider>
    </TooltipProvider>,
  );
}

const cards = (html: string) => html.match(/data-testid="agent-session-card"/g)?.length ?? 0;
const cardIds = (html: string) => [...html.matchAll(/data-testid="agent-session-card" data-session-id="([^"]+)"/g)].map((m) => m[1]);
const cardHtml = (html: string, id: string) => {
  const at = html.indexOf(`data-session-id="${id}"`);
  // Up to where the next card's element opens, so one card's classes are not another's.
  const next = html.indexOf('<div role="listitem"', at);
  return html.slice(at, next === -1 ? undefined : next);
};
/** The class list of a card's own element (not its children's). */
const rootClass = (html: string, id: string) => cardHtml(html, id).match(/^[^>]*class="([^"]*)"/)?.[1] ?? "";

/** The deepest a <button> sits inside another <button>: 1 means none nest. */
function buttonDepth(html: string): number {
  let depth = 0;
  let max = 0;
  for (const tag of html.matchAll(/<(\/?)button\b/g)) {
    depth += tag[1] ? -1 : 1;
    max = Math.max(max, depth);
  }
  return max;
}

describe("groups", () => {
  it("one group per workspace / folder, each with its name and a light count", () => {
    const html = render({
      sessions: [session("a", { projectPath: "/work/api" }), session("b", { projectPath: "/work/web" }), session("c", { projectPath: "/work/web" })],
    });
    expect(html.match(/data-testid="agent-session-group"/g)).toHaveLength(2);
    expect(html).toContain('data-group-key="project:/work/api"');
    expect(html).toMatch(/data-testid="agent-group-count"[^>]*>2</);
  });

  it("the label is small and quiet, its count a superscript", () => {
    const html = render({ sessions: aged(3) });
    expect(html).toMatch(/data-testid="agent-group-name" class="[^"]*text-caption-11 text-text-tertiary/);
    expect(html).toMatch(/<sup data-testid="agent-group-count"[^>]*>3</);
    // The label of the group on screen reads a step stronger.
    const active = render({ sessions: aged(3), activeSessionId: "s1" });
    expect(active).toMatch(/data-testid="agent-group-name" class="[^"]*text-text-secondary/);
  });

  it("a workspace of several folders says each row's folder; of one, it does not", () => {
    const several = render({
      sessions: [session("a", { projectPath: "/work/web", workspaceId: "ws1", preview: "Done." })],
      workspaces: [{ id: "ws1", name: "Billing", paths: ["/work/api", "/work/web"] }],
    });
    expect(cardHtml(several, "a")).toMatch(/opacity-70">web · <\/span>Done\./);
    const one = render({
      sessions: [session("a", { projectPath: "/work/web", workspaceId: "ws1", preview: "Done." })],
      workspaces: [{ id: "ws1", name: "Billing", paths: ["/work/web"] }],
    });
    expect(cardHtml(one, "a")).not.toContain("opacity-70");
  });

  it("names the workspace, and puts no-project last", () => {
    const html = render({
      sessions: [session("a", { projectPath: "/cfg/s/1" }), session("b", { projectPath: "/work/api", workspaceId: "ws1" })],
      scratchRoot: "/cfg/s",
      workspaces: [{ id: "ws1", name: "Billing", paths: ["/work/api"] }],
    });
    expect(html.indexOf("Billing")).toBeLessThan(html.indexOf("No project"));
    expect(html).toContain('data-group-key="workspace:ws1"');
  });

  it("a group folded shut draws no cards, says it is collapsed, and keeps the waiting badge", () => {
    const sessions = [session("a", { status: "awaiting_permission" }), session("b"), session("c")];
    const html = render({
      sessions,
      collapsedKeys: ["project:/work/api"],
      attention: attentionBySession([{ sessionId: "a" }], [{ sessionId: "b" }]),
    });
    expect(cards(html)).toBe(0);
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain('data-collapsed="true"');
    // Two sessions need the person; the number and the words both say so.
    expect(html).toMatch(/data-testid="agent-group-waiting"[^>]*><span[^>]*>2<\/span>/);
    expect(html).toContain("2 sessions need your response");
  });

  it("an open group's header is aria-expanded and has no badge when nothing waits", () => {
    const html = render({ sessions: aged(2) });
    expect(html).toContain('aria-expanded="true"');
    expect(html).not.toContain('data-testid="agent-group-waiting"');
  });

  it("the + of a group is there to be tabbed to, and so is the one in the header", () => {
    const html = render({ sessions: aged(1), onNewSessionIn: noop });
    expect(html).toContain('data-testid="agent-tree-project-new"');
    expect(html).toContain('aria-label="New session in api"');
    expect(html).toContain("group-focus-within/header:opacity-100");
    // With every document tab closed the header's + is still there.
    expect(render({ sessions: [] })).toContain('data-testid="agent-tree-new"');
    expect(render({ sessions: aged(1) })).not.toContain('data-testid="agent-tree-project-new"');
  });

  it("says so when there is nothing, and when only closed ones are hidden", () => {
    expect(render()).toContain("No sessions yet");
    const hidden = render({ sessions: [session("a", { status: "closed" })], hideClosed: true });
    expect(hidden).toContain("Only closed sessions");
    expect(cards(hidden)).toBe(0);
  });
});

describe("five recent and the stack", () => {
  it("draws five and says how many are folded", () => {
    const html = render({ sessions: aged(8) });
    expect(cardIds(html)).toEqual(["s0", "s1", "s2", "s3", "s4"]);
    expect(html).toContain("3 older sessions");
    expect(html).toContain('data-testid="agent-stack-toggle"');
    expect(html).toMatch(/data-testid="agent-stack-toggle"[^>]*aria-expanded="false"/);
  });

  it("opening the stack draws the rest below, with a way back, and the count of the group is unchanged", () => {
    const html = render({ sessions: aged(8), olderOpenKeys: ["project:/work/api"] });
    expect(cardIds(html)).toEqual(["s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7"]);
    expect(html).toContain("Hide older sessions");
    expect(html).toMatch(/data-testid="agent-stack-toggle"[^>]*aria-expanded="true"/);
    expect(html).toMatch(/data-testid="agent-group-count"[^>]*>8</);
  });

  it("opening the stack changes no selection", () => {
    const closed = render({ sessions: aged(8), activeSessionId: "s1" });
    const open = render({ sessions: aged(8), activeSessionId: "s1", olderOpenKeys: ["project:/work/api"] });
    expect(cardHtml(closed, "s1")).toContain('aria-current="true"');
    expect(cardHtml(open, "s1")).toContain('aria-current="true"');
    expect(open.match(/aria-current="true"/g)).toHaveLength(1);
  });

  it("the stack is a line of text and a hairline, not layered cards", () => {
    const html = render({ sessions: aged(22) });
    const toggle = html.slice(html.indexOf('data-testid="agent-stack-toggle"'));
    const row = toggle.slice(0, toggle.indexOf("</button>"));
    expect(row).toContain(">17 older sessions<");
    expect(row).toContain('data-testid="agent-stack-rule"');
    expect(html).not.toContain("bg-surface-raised/50");
  });

  it("no stack row at five", () => {
    expect(render({ sessions: aged(5) })).not.toContain('data-testid="agent-stack-toggle"');
  });

  it("the session on screen is drawn though old, once, and the stack count drops with it", () => {
    const html = render({ sessions: aged(8), activeSessionId: "s7" });
    expect(cardIds(html)).toEqual(["s0", "s1", "s2", "s3", "s4", "s7"]);
    expect(html).toContain("2 older sessions");
  });

  it("while the stack is open the session on screen keeps its place, so selecting it does not move (remount) its card", () => {
    const closed = render({ sessions: aged(8), activeSessionId: "s7" });
    expect(cardIds(closed)).toEqual(["s0", "s1", "s2", "s3", "s4", "s7"]);
    const open = render({ sessions: aged(8), activeSessionId: "s7", olderOpenKeys: ["project:/work/api"] });
    // Same order as with nothing selected: s7 is still the last of the stack.
    expect(cardIds(open)).toEqual(cardIds(render({ sessions: aged(8), olderOpenKeys: ["project:/work/api"] })));
    expect(cardHtml(open, "s7")).toContain('aria-current="true"');
  });

  it("hiding closed sessions keeps a child in its parent's group", () => {
    const html = render({
      sessions: [
        session("parent", { status: "closed", projectPath: "/a", workspaceId: "wa" }),
        session("child", { parentSessionId: "parent", depth: 1, projectPath: "/b", workspaceId: "wb" }),
      ],
      workspaces: [
        { id: "wa", name: "Alpha", paths: ["/a"] },
        { id: "wb", name: "Beta", paths: ["/b"] },
      ],
      hideClosed: true,
    });
    expect(html).toContain('data-group-key="workspace:wa"');
    expect(html).not.toContain('data-group-key="workspace:wb"');
    expect(cardIds(html)).toEqual(["child"]);
  });

  it("a waiting session is drawn though old: by queue as well as by status", () => {
    const byStatus = render({ sessions: [...aged(7), session("w", { status: "awaiting_permission", updatedAt: 1 })] });
    expect(cardIds(byStatus)).toContain("w");
    const byQueue = render({ sessions: aged(8), attention: attentionBySession([], [{ sessionId: "s7" }]) });
    expect(cardIds(byQueue)).toContain("s7");
    expect(byQueue).toContain("2 older sessions");
  });

  it("pinned sessions go first and stay drawn", () => {
    const html = render({ sessions: aged(8), pinnedIds: ["s6"] });
    expect(cardIds(html)[0]).toBe("s6");
    expect(cardIds(html)).toHaveLength(6);
    // Said in the card's description, not drawn as a glyph in the row.
    expect(cardHtml(html, "s6")).toMatch(/data-testid="agent-card-description"[^>]*>[^<]*Pinned/);
    expect(cardHtml(html, "s0")).not.toContain("Pinned");
  });

  it("an exception drawn from the stack is not drawn twice when the stack opens", () => {
    const html = render({
      sessions: aged(8),
      activeSessionId: "s6",
      pinnedIds: ["s5"],
      olderOpenKeys: ["project:/work/api"],
    });
    const ids = cardIds(html);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toHaveLength(8);
  });

  it("running sessions are not pulled out of the stack", () => {
    const html = render({ sessions: aged(12, { status: "running" }) });
    expect(cards(html)).toBe(5);
    expect(html).toContain("7 older sessions");
  });

  it("hide-closed leaves the one on screen and counts only what is left", () => {
    const sessions = [...aged(2), session("c1", { status: "closed", updatedAt: 5 }), session("c2", { status: "closed", updatedAt: 4 })];
    const html = render({ sessions, hideClosed: true, activeSessionId: "c2" });
    expect(cardIds(html)).toEqual(["s0", "s1", "c2"]);
  });
});

describe("a card", () => {
  it("is two lines: the title, then the second line, with the title larger", () => {
    const html = render({ sessions: [session("a", { title: "Fix the flaky retry test" })] });
    expect(html).toContain("Fix the flaky retry test");
    expect(html).toMatch(/data-testid="agent-card-title" class="[^"]*text-body-13/);
    // One line, cut with an ellipsis rather than shrunk.
    expect(html).toMatch(/data-testid="agent-card-title" class="[^"]*\btruncate\b/);
  });

  it("a row is two fixed lines: title + age, then the second line", () => {
    const html = render({ sessions: [session("a", { status: "idle", preview: "Done." })] });
    const card = cardHtml(html, "a");
    expect(card).toContain("h-12.5");
    expect(card).toContain('data-testid="agent-card-title"');
    expect(card).toContain('data-testid="agent-card-line"');
    expect(card).toContain(">Done.<");
    expect(card).toContain('data-slot="session-avatar"');
  });

  it("an empty second line still holds its place", () => {
    const card = cardHtml(render({ sessions: [session("a")] }), "a");
    expect(card).toMatch(/data-testid="agent-card-line"[^>]*>\u00a0</);
  });

  it("a waiting row says what it waits for, in the warning tone", () => {
    const html = renderCard({
      session: session("a", { status: "awaiting_permission" }),
      attention: { permissions: 1, questions: 0 },
      ask: "Allow migrate up?",
    });
    expect(cardHtml(html, "a")).toMatch(/agent-card-line[^>]*text-status-warning[^>]*>[^<]*Allow migrate up\?/);
  });

  it("a failed row says where it stopped, in the danger tone, and a folder goes first", () => {
    const html = renderCard({ session: session("a", { status: "error", preview: "Build broke." }), folder: "web" });
    expect(html).toMatch(/agent-card-line[^>]*text-status-danger/);
    expect(html).toMatch(/opacity-70">web · <\/span>Build broke\./);
  });

  it("draws nothing for idle: no state on the face, no turn count", () => {
    const html = cardHtml(render({ sessions: [session("a")] }), "a");
    expect(html).toContain('data-state="idle"');
    expect(html).not.toContain('data-slot="agent-state"');
    expect(html).not.toMatch(/\bturns?\b/);
    expect(html).not.toContain("data-waiting");
  });

  it("marks running, error, permission and question on the face", () => {
    const html = render({
      sessions: [
        session("run", { status: "running", updatedAt: 5 }),
        session("err", { status: "error", updatedAt: 4 }),
        session("perm", { status: "awaiting_permission", updatedAt: 3 }),
        session("ask", { status: "running", updatedAt: 2 }),
      ],
      attention: attentionBySession([], [{ sessionId: "ask" }]),
    });
    expect(cardHtml(html, "run")).toContain('data-state="running"');
    expect(cardHtml(html, "err")).toContain('data-state="error"');
    expect(cardHtml(html, "perm")).toContain('data-state="waiting"');
    expect(cardHtml(html, "ask")).toContain('data-state="waiting"');
  });

  it("the face's motion is its own; the one-off beat does not play on first paint", () => {
    const run = cardHtml(render({ sessions: [session("a", { status: "running" })] }), "a");
    expect(run).toContain("agent-avatar-spin");
    const wait = cardHtml(render({ sessions: [session("a", { status: "awaiting_permission" })] }), "a");
    expect(wait).toContain("agent-avatar-breathe");
    expect(wait).not.toContain("animate-pulse");
    expect(wait).not.toContain("animate-spin");
    expect(wait).not.toContain("agent-attention-pulse");
    expect(wait).not.toContain("agent-attention-flash");
    expect(wait).not.toContain("shimmer");
  });

  it("a card waiting on the person says so on the card", () => {
    const html = cardHtml(render({ sessions: [session("a", { status: "awaiting_permission" })] }), "a");
    expect(html).toContain('data-waiting="true"');
  });

  it("selected is a raised ground, no ring, and aria-current", () => {
    const html = render({ sessions: aged(2), activeSessionId: "s1" });
    const selected = cardHtml(html, "s1");
    expect(selected).toContain('data-selected="true"');
    expect(selected).toContain('aria-current="true"');
    expect(rootClass(html, "s1")).toContain("bg-surface-raised");
    // The face keeps its own hairline; the row draws no ring.
    expect(rootClass(html, "s1")).not.toContain("ring");
    const other = cardHtml(html, "s0");
    expect(other).not.toContain("aria-current");
    expect(rootClass(html, "s0")).not.toContain("bg-surface-raised");
  });

  it("names the card in words for a screen reader and for a keyboard's tooltip", () => {
    const html = render({
      sessions: [session("a", { status: "running", updatedAt: Date.now() - 10 * 60_000 })],
      subagents: { a: [{ id: "x", name: "Docs" }] },
      pinnedIds: ["a"],
    });
    const text = html.match(/data-testid="agent-card-description" class="sr-only">([^<]*)</)?.[1] ?? "";
    expect(text).toContain("Claude");
    expect(text).toContain("Running");
    expect(text).toContain("1 subagent: Docs, running");
    expect(text).toContain("Pinned");
    expect(html).toMatch(/data-testid="agent-card-open"[^>]*aria-describedby=/);
  });

  it("says a child's parent in its description and draws it as a card of its own", () => {
    const html = render({
      sessions: [session("root", { updatedAt: 9 }), session("kid", { parentSessionId: "root", depth: 1, updatedAt: 8 })],
    });
    expect(cardIds(html)).toEqual(["root", "kid"]);
    expect(cardHtml(html, "kid")).toMatch(/data-testid="agent-card-description"[^>]*>[^<]*Child of Title root/);
    expect(cardHtml(html, "root")).not.toContain("Child of");
    // Flat: no tree role, no indentation.
    expect(html).not.toContain('role="tree"');
    expect(html).not.toContain('role="group"');
  });

  it("shows the age when there is room, and drops it when the time is unknown", () => {
    expect(render({ sessions: [session("a", { updatedAt: Date.now() - 2 * 3_600_000 - 5 * 60_000 })] })).toContain(">2h<");
    expect(render({ sessions: [session("a")] })).not.toContain('data-testid="agent-card-age"');
  });

  it("has a menu button with a name, shown on hover and keyboard focus", () => {
    const html = cardHtml(render({ sessions: [session("a", { title: "Planner" })] }), "a");
    expect(html).toContain('aria-label="Actions for Planner"');
    expect(html).toContain("group-hover/card:opacity-100");
    expect(html).toContain("group-focus-within/card:opacity-100");
    expect(html).toContain("opacity-0");
  });

  it("opens by the whole card and by keyboard through one button, with the card's ring on focus", () => {
    const html = cardHtml(render({ sessions: [session("a")] }), "a");
    expect(html).toContain('data-testid="agent-card-open"');
    expect(html).toContain("after:inset-0");
    expect(html).toContain("focus-visible:after:ring-2");
  });

  it("never nests one button in another, with stack, menu and groups on screen", () => {
    const html = render({
      sessions: aged(9),
      subagents: { s0: [1, 2, 3, 4, 5].map((n) => ({ id: `a${n}`, name: `Agent ${n}` })) },
      pinnedIds: ["s1"],
      onNewSessionIn: noop,
      onToggleHideClosed: noop,
    });
    expect(buttonDepth(html)).toBe(1);
  });
});

describe("subagent avatars", () => {
  const five: Subagent[] = [1, 2, 3, 4, 5].map((n) => ({ id: `a${n}`, name: `Agent ${n}`, state: n === 3 ? "failed" : n === 5 ? "completed" : undefined }));
  const avatars = (max: number, list = five) =>
    renderToString(
      <TooltipProvider>
        <AgentSubagentAvatars subagents={list} max={max} onOpen={noop} />
      </TooltipProvider>,
    );

  it("shows up to three avatars and the rest as +N", () => {
    const html = avatars(3);
    expect(html.match(/data-testid="agent-subagent-avatar"/g)).toHaveLength(3);
    expect(html).toContain("+2");
    expect(html).toContain('aria-label="5 subagents, show all"');
  });

  it("two when the rail is narrow", () => {
    const html = avatars(2);
    expect(html.match(/data-testid="agent-subagent-avatar"/g)).toHaveLength(2);
    expect(html).toContain("+3");
  });

  it("no +N when they all fit", () => {
    const html = avatars(3, five.slice(0, 3));
    expect(html.match(/data-testid="agent-subagent-avatar"/g)).toHaveLength(3);
    expect(html).not.toContain("agent-subagent-more");
  });

  it("the group is named in full, with every state, and sits at 16px", () => {
    const html = avatars(3);
    expect(html).toContain('role="group"');
    expect(html).toContain("5 subagents: Agent 1, running; Agent 2, running; Agent 4, running; Agent 3, failed; Agent 5, done");
    expect(html).toContain("size-4");
  });

  it("running ones come first, so the avatars that fit are the ones worth a glance", () => {
    const html = avatars(3);
    const shown = [...html.matchAll(/aria-label="Open ([^"]+)"/g)].map((m) => m[1]);
    expect(shown).toEqual(["Agent 1, running", "Agent 2, running", "Agent 4, running"]);
  });

  it("uses the subagent's own initials, and a bot glyph when it never gave a name", () => {
    const html = avatars(3, [{ id: "abcdef123", name: "Review whitelist" }, { id: "zzzzzz999" }]);
    expect(html).toContain(">R<");
    expect(html).toContain("lucide-bot");
    // No picture is invented for either.
    expect(html).not.toContain("<img");
  });

  it("renders nothing for no subagents", () => {
    expect(avatars(3, [])).toBe("");
  });
});

describe("tabs, selection and resources", () => {
  const tabs = () => moduleTabs(useWorkbenchTabsStore.getState().modules, "agenthub");

  beforeEach(() => {
    useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "notes" });
    useAgentSessionsStore.setState({ sessionsById: {}, sessionIds: [], activeSessionId: "", chatOpen: false });
    useSubagentPanel.setState({ open: null });
    useAgentPinsStore.setState({ olderOpen: [], collapsed: [], expanded: [] });
  });

  it("choosing a subagent opens its session's tab and that subagent's panel", () => {
    showSubagent(session("a"), { id: "sub-2" });
    expect(useSubagentPanel.getState().open).toEqual({ sessionId: "a", subagentId: "sub-2" });
    expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("a");
    // Asked again for another subagent of the same session: one tab, the panel moves.
    showSubagent(session("a"), { id: "sub-3" });
    expect(tabs().openTabs).toHaveLength(1);
    expect(useSubagentPanel.getState().open).toEqual({ sessionId: "a", subagentId: "sub-3" });
  });

  it("the highlighted card follows the focused group, not the module's last active tab", () => {
    openAgentSession(session("a"));
    openAgentSession(session("b"));
    useWorkbenchTabsStore.getState().splitTab(tabs().openTabs[1].id);
    expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe("b");
    const first = useWorkbenchTabsStore.getState().groups.find((g) => g.activeTabId === tabs().openTabs[0].id)!;
    useWorkbenchTabsStore.getState().focusGroup(first.id);
    const active = activeAgentHubSessionId(useWorkbenchTabsStore.getState());
    expect(active).toBe("a");
    const html = render({ sessions: [session("a", { updatedAt: 2 }), session("b", { updatedAt: 1 })], activeSessionId: active });
    expect(cardHtml(html, "a")).toContain('aria-current="true"');
    expect(cardHtml(html, "b")).not.toContain("aria-current");
  });

  it("closing a tab closes no session and drops nothing the session holds", () => {
    useAgentSessionsStore.getState().upsertSession(session("a", { status: "running" }));
    useAgentSessionsStore.getState().trackSubagent("a", { id: "x", name: "Docs" });
    openAgentSession(session("a"));
    closeAgentSessionTabs(["a"]);
    expect(tabs().openTabs).toHaveLength(0);
    const state = useAgentSessionsStore.getState();
    expect(state.sessionsById.a?.status).toBe("running");
    expect(state.subagents.a).toHaveLength(1);
    // And the rail still lists it, with no tab open.
    expect(cardIds(render({ sessions: [state.sessionsById.a] }))).toEqual(["a"]);
  });

  it("opening a stack is a preference of the group: it touches no tab and no selection", () => {
    openAgentSession(session("s1"));
    const before = activeAgentHubSessionId(useWorkbenchTabsStore.getState());
    useAgentPinsStore.getState().toggleOlderOpen("project:/work/api");
    expect(useAgentPinsStore.getState().olderOpen).toEqual(["project:/work/api"]);
    expect(activeAgentHubSessionId(useWorkbenchTabsStore.getState())).toBe(before);
    expect(tabs().openTabs).toHaveLength(1);
    useAgentPinsStore.getState().toggleOlderOpen("project:/work/api");
    expect(useAgentPinsStore.getState().olderOpen).toEqual([]);
  });

  it("the stack stays closed for a group nobody opened it for", () => {
    useAgentPinsStore.getState().toggleOlderOpen("project:/work/api");
    const html = render({
      sessions: [...aged(8), ...aged(8).map((s) => ({ ...s, id: `w${s.id}`, projectPath: "/work/web" }))],
      olderOpenKeys: useAgentPinsStore.getState().olderOpen,
    });
    // api open (8 cards), web folded (5): the key is the group's, not global.
    expect(cards(html)).toBe(13);
  });

  it("expanded keys toggle and persist like collapsed", () => {
    const { toggleExpanded } = useAgentPinsStore.getState();
    toggleExpanded("workspace:2");
    expect(useAgentPinsStore.getState().expanded).toEqual(["workspace:2"]);
    toggleExpanded("workspace:2");
    expect(useAgentPinsStore.getState().expanded).toEqual([]);
  });
});
