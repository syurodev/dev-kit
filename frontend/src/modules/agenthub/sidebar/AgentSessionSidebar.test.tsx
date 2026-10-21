import { beforeEach, describe, expect, it } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
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
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionActionsContext, SessionMenuItems, subagentMenuEntries, type MenuKit, type SessionActions } from "./actions";
import { AgentSessionCard, type AgentSessionCardProps } from "./AgentSessionCard";
import { AgentSessionSidebar, type AgentSessionSidebarProps } from "./AgentSessionSidebar";
import { attentionBySession } from "./model";
import { showSubagent } from "./openSubagent";
import { SessionBranch, type BranchContext } from "./SessionBranch";
import { buildForest } from "./tree";

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
        <AgentSessionCard selected={false} pinned={false} subagents={[]} ask={null} now={Date.now()} {...over} />
      </SessionActionsContext.Provider>
    </TooltipProvider>,
  );
}

/** A session in workspace `wsId`, and that workspace (folder `/ws/<id>`). */
const inWs = (id: string, wsId: string, over: Partial<AgentSession> = {}) =>
  session(id, { projectPath: `/ws/${wsId}`, workspaceId: wsId, ...over });
const ws = (id: string) => ({ id, name: `Workspace ${id}`, paths: [`/ws/${id}`] });

const cards = (html: string) => html.match(/data-testid="agent-session-card"/g)?.length ?? 0;
const cardIdsIn = (html: string) => [...html.matchAll(/data-testid="agent-session-card" data-session-id="([^"]+)"/g)].map((m) => m[1]);
const cardIds = (html: string) => [...html.matchAll(/data-testid="agent-session-card" data-session-id="([^"]+)"/g)].map((m) => m[1]);
const cardHtml = (html: string, id: string) => {
  const at = html.indexOf(`data-session-id="${id}"`);
  // Up to where the next card's element opens, so one card's classes are not another's.
  const next = html.indexOf('<div role="listitem"', at);
  return html.slice(at, next === -1 ? undefined : next);
};
/** The class list of a card's own element (not its children's). */
const rootClass = (html: string, id: string) => cardHtml(html, id).match(/^[^>]*class="([^"]*)"/)?.[1] ?? "";

/** The opening tag of the element carrying `testid` (attributes in any order). */
const tagOf = (html: string, testid: string) => {
  const at = html.indexOf(`data-testid="${testid}"`);
  return at === -1 ? "" : html.slice(html.lastIndexOf("<", at), html.indexOf(">", at) + 1);
};

/** A group's header: from its section to the end of its toggle button. */
const header = (html: string, key: string) => {
  const at = html.indexOf(`data-group-key="${key}"`);
  const toggle = html.indexOf('data-testid="agent-group-toggle"', at);
  return html.slice(html.lastIndexOf("<button", toggle), html.indexOf("</button>", toggle));
};

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

  it("the header is one quiet row: chevron, name, and the count on the right — not a superscript", () => {
    const html = render({ sessions: aged(3) });
    const toggle = header(html, "project:/work/api");
    const cls = (toggle.match(/^<button[^>]*class="([^"]*)"/)?.[1] ?? "").split(" ");
    for (const c of ["h-8", "px-2", "rounded-md", "hover:bg-state-soft", "focus-visible:ring-2"]) expect(cls).toContain(c);
    expect(html).toMatch(/data-testid="agent-group-name" class="[^"]*text-body-13 font-medium text-text-secondary/);
    expect(html).not.toContain("<sup");
    expect(html).toMatch(/data-testid="agent-group-count" aria-hidden="true" class="[^"]*text-caption-11 tabular-nums text-text-tertiary[^"]*">3</);
    expect(html).toContain("3 sessions");
    // The label of the group on screen reads a step stronger.
    const active = render({ sessions: aged(3), activeSessionId: "s1" });
    expect(active).toMatch(/data-testid="agent-group-name" class="[^"]*text-text-primary/);
  });

  it("the chevron is always there, and turns when the group is open", () => {
    const chevron = (html: string) => html.match(/<svg[^>]*data-testid="agent-group-chevron"[^>]*>/)?.[0] ?? "";
    const open = chevron(render({ sessions: aged(2) }));
    expect(open).toContain("rotate-90");
    expect(open).not.toContain("opacity-0");
    expect(open).toContain("motion-reduce:transition-none");
    const shut = chevron(render({ sessions: aged(2), collapsedKeys: ["project:/work/api"] }));
    expect(shut).not.toContain("rotate-90");
    expect(shut).not.toContain("opacity-0");
  });

  it("the group is a Collapsible: Radix wires the header to its body, and the body animates", () => {
    const html = render({ sessions: aged(2) });
    expect(html).toMatch(/data-testid="agent-session-group"[^>]*data-state="open"/);
    const id = header(html, "project:/work/api").match(/aria-controls="([^"]+)"/)?.[1];
    expect(id).toBeTruthy();
    const body = html.match(new RegExp(`<div[^>]*id="${id}"[^>]*>`))?.[0] ?? "";
    expect(body).toContain('data-slot="collapsible-content"');
    expect(body).toContain("data-open:animate-collapsible-down");
    // Folded, the body draws nothing: an empty, hidden element and no cards.
    const shut = render({ sessions: aged(2), collapsedKeys: ["project:/work/api"] });
    expect(shut).toMatch(/<div data-state="closed" id="[^"]+" hidden="" data-slot="collapsible-content"[^>]*><\/div>/);
    expect(cards(shut)).toBe(0);
  });

  it("the + takes the count's place on hover, so nothing beside it moves", () => {
    const html = render({ sessions: aged(3), onNewSessionIn: noop });
    // The count's slot is at least as wide as the + and gives way to it.
    const slot = header(html, "project:/work/api").match(/<span aria-hidden="true" class="([^"]*)"><span data-testid="agent-group-count"/)?.[1] ?? "";
    expect(slot).toContain("min-w-(--control-height-xs)");
    expect(slot).toContain("group-hover/header:opacity-0");
    expect(slot).toContain("group-focus-within/header:opacity-0");
    expect(html).toMatch(/data-testid="agent-tree-project-new"[^>]*class="[^"]*\babsolute\b/);
    // Without a + there is nothing to make room for.
    expect(header(render({ sessions: aged(3) }), "project:/work/api")).not.toContain("group-hover/header:opacity-0");
  });

  it("a row never says its folder, whatever the workspace holds", () => {
    const several = render({
      sessions: [session("a", { projectPath: "/work/web", workspaceId: "ws1", preview: "Done." })],
      workspaces: [{ id: "ws1", name: "Billing", paths: ["/work/api", "/work/web"] }],
    });
    expect(cardHtml(several, "a")).toMatch(/agent-card-line[^>]*>Done\.<\/span>/);
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

  it("a group folded shut draws no cards of its own; the ones waiting are still drawn, above it", () => {
    const sessions = [session("a", { status: "awaiting_permission" }), session("b"), session("c")];
    const html = render({
      sessions,
      collapsedKeys: ["project:/work/api"],
      attention: attentionBySession([{ sessionId: "a" }], [{ sessionId: "b" }]),
    });
    expect(html).toContain('aria-expanded="false"');
    expect(html).toContain('data-collapsed="true"');
    // Two sessions need the person: drawn once, in Needs you, whatever their group does.
    expect(cardIds(html).sort()).toEqual(["a", "b"]);
    expect(html.indexOf('data-testid="agent-needs-you"')).toBeLessThan(html.indexOf('data-testid="agent-session-group"'));
  });

  it("an open group's header is aria-expanded", () => {
    const html = render({ sessions: aged(2) });
    expect(html).toContain('aria-expanded="true"');
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
    expect(tagOf(html, "agent-stack-toggle")).toContain('aria-expanded="false"');
  });

  it("opening the stack draws the rest below, with a way back, and the count of the group is unchanged", () => {
    const html = render({ sessions: aged(8), olderOpenKeys: ["project:/work/api"] });
    expect(cardIds(html)).toEqual(["s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7"]);
    expect(html).toContain("Hide older sessions");
    expect(tagOf(html, "agent-stack-toggle")).toContain('aria-expanded="true"');
    // A Collapsible of its own: Radix points the row at the list it opens.
    const id = tagOf(html, "agent-stack-toggle").match(/aria-controls="([^"]+)"/)?.[1];
    const list = html.match(new RegExp(`<div[^>]*id="${id}"[^>]*>`))?.[0] ?? "";
    expect(list).toContain('data-slot="collapsible-content"');
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

  it("a failed row says where it stopped, in the danger tone", () => {
    const html = renderCard({ session: session("a", { status: "error", preview: "Build broke." }) });
    expect(html).toMatch(/agent-card-line[^>]*text-status-danger[^>]*>Build broke\./);
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

describe("tabs, selection and resources", () => {
  const tabs = () => moduleTabs(useWorkbenchTabsStore.getState().modules, "agenthub");

  beforeEach(() => {
    useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "notes", recentlyClosed: [] });
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

describe("needs you, and the workspace in front of you", () => {
  it("sessions that need you are drawn once, on top", () => {
    const sessions = [
      inWs("a", "1", { status: "idle", updatedAt: 1 }),
      inWs("b", "1", { status: "error", updatedAt: 3 }),
      inWs("c", "1", { status: "running", updatedAt: 2 }),
    ];
    const html = render({
      sessions,
      workspaces: [ws("1")],
      attention: { c: { permissions: 1, questions: 0 } },
      asks: { c: "Allow?" },
      currentWorkspaceId: "1",
    });
    expect(html).toContain('data-testid="agent-needs-you"');
    const needs = html.slice(html.indexOf('data-testid="agent-needs-you"'), html.indexOf('data-testid="agent-session-group"'));
    expect(cardIdsIn(needs)).toEqual(["b", "c"]);
    expect(cardIds(html).filter((id) => id === "c").length).toBe(1);
    expect(cardHtml(html, "c")).toMatch(/agent-card-line[^>]*text-status-warning[^>]*>[^<]*Allow\?/);
    // Its label is the same row as a group's header: no superscript count.
    expect(needs).not.toContain("<sup");
    expect(needs).toMatch(/text-caption-11 tabular-nums text-text-tertiary">2</);
    // The group keeps only what does not wait, and counts only that.
    expect(html).toMatch(/data-testid="agent-group-count"[^>]*>1</);
  });

  it("the current workspace's group comes first, whatever its name", () => {
    const html = render({
      sessions: [inWs("x", "1"), inWs("y", "2")],
      workspaces: [
        { id: "1", name: "Alpha", paths: ["/ws/1"] },
        { id: "2", name: "Zeta", paths: ["/ws/2"] },
      ],
      currentWorkspaceId: "2",
    });
    expect(html.indexOf('data-group-key="workspace:2"')).toBeLessThan(html.indexOf('data-group-key="workspace:1"'));
    // With none chosen, the names decide.
    const none = render({
      sessions: [inWs("x", "1"), inWs("y", "2")],
      workspaces: [
        { id: "1", name: "Alpha", paths: ["/ws/1"] },
        { id: "2", name: "Zeta", paths: ["/ws/2"] },
      ],
    });
    expect(none.indexOf('data-group-key="workspace:1"')).toBeLessThan(none.indexOf('data-group-key="workspace:2"'));
  });

  it("needs-you sits inside a collapse, so it eases in and out rather than popping", () => {
    const html = render({ sessions: [session("a", { status: "error" })] });
    const before = html.slice(html.indexOf('data-testid="agent-session-viewport"'), html.indexOf('data-testid="agent-needs-you"'));
    expect(before).toContain('data-slot="animated-collapse"');
  });

  it("no needs-you section when nothing waits", () => {
    expect(render({ sessions: [session("a", { status: "idle" })] })).not.toContain("agent-needs-you");
  });

  it("a waiting parent leaves its group, and its child stays in that group", () => {
    const html = render({
      sessions: [
        inWs("p", "1", { status: "awaiting_permission", updatedAt: 2 }),
        session("kid", { parentSessionId: "p", depth: 1, projectPath: "/elsewhere", updatedAt: 1 }),
      ],
      workspaces: [ws("1")],
    });
    expect(html).toContain('data-group-key="workspace:1"');
    expect(html).not.toContain('data-group-key="project:/elsewhere"');
  });

  it("other workspaces fold by default", () => {
    const html = render({ sessions: [inWs("x", "2")], workspaces: [ws("1"), ws("2")], currentWorkspaceId: "1" });
    expect(html).toMatch(/data-group-key="workspace:2"[^>]*data-collapsed="true"/);
    expect(cards(html)).toBe(0);
  });

  it("the current workspace opens by default, and another opens when expanded", () => {
    const html = render({
      sessions: [inWs("x", "1"), inWs("y", "2")],
      workspaces: [ws("1"), ws("2")],
      currentWorkspaceId: "1",
      expandedKeys: ["workspace:2"],
    });
    expect(cardIds(html).sort()).toEqual(["x", "y"]);
  });

  it("every header is the same row, folded or open: no big jump above the open one", () => {
    const html = render({
      sessions: [inWs("x", "1"), inWs("y", "2")],
      workspaces: [ws("1"), ws("2")],
      currentWorkspaceId: "1",
    });
    for (const key of ["workspace:1", "workspace:2"]) {
      expect(header(html, key)).toMatch(/class="[^"]*\bh-8\b/);
      expect(html.slice(html.indexOf(`data-group-key="${key}"`))).not.toMatch(/^[^>]*pt-4/);
    }
    expect(html).not.toContain("pt-4 pb-1.5");
  });

  it("a folded workspace says how many of its sessions run", () => {
    const html = render({
      sessions: [inWs("x", "2", { status: "running" }), inWs("y", "2", { status: "running" }), inWs("z", "2")],
      workspaces: [ws("1"), ws("2")],
      currentWorkspaceId: "1",
    });
    expect(html).toContain('data-testid="agent-group-running"');
    expect(html).toMatch(/data-testid="agent-group-running"[^>]*>.*?2 running</);
    // Running says more than the total, so it stands in the count's place.
    const folded = html.slice(html.indexOf('data-group-key="workspace:2"'));
    expect(folded.slice(0, folded.indexOf("</section>"))).not.toContain('data-testid="agent-group-count"');
    // Open, the rows say it themselves.
    const open = render({
      sessions: [inWs("x", "2", { status: "running" })],
      workspaces: [ws("1"), ws("2")],
      currentWorkspaceId: "1",
      expandedKeys: ["workspace:2"],
    });
    expect(open).not.toContain('data-testid="agent-group-running"');
  });

  it("the rail is titled Agents, with New beside it", () => {
    const html = render({ sessions: [session("a")], onToggleHideClosed: noop });
    expect(html).toMatch(/<h2[^>]*>Agents<\/h2>/);
    expect(html).toMatch(/data-testid="agent-tree-new"[^>]*>.*?New<\/button>/);
    expect(html).toContain('data-testid="agent-tree-toggle-closed"');
  });
});

describe("subagents in a row's menu", () => {
  const agents: Subagent[] = [
    { id: "a1", name: "Docs", state: "completed" },
    { id: "a2", name: "Review" },
  ];

  it("one entry per subagent, running first, with its name and state", () => {
    expect(subagentMenuEntries(agents).map(({ label, detail }) => ({ label, detail }))).toEqual([
      { label: "Review", detail: "running" },
      { label: "Docs", detail: "done" },
    ]);
    expect(subagentMenuEntries([{ id: "abcdef123" }])[0].label).toBe("Subagent abcdef");
    expect(subagentMenuEntries([])).toEqual([]);
  });

  const kit: MenuKit = {
    Item: ({ children, detail }) => (
      <button type="button" data-detail={typeof detail === "string" ? detail : undefined}>
        {children}
      </button>
    ),
    Separator: () => <hr />,
    Label: ({ children }) => <p data-label="">{children}</p>,
  };
  const menu = (subagents: readonly Subagent[]) =>
    renderToString(
      <QueryClientProvider client={new QueryClient()}>
        <SessionActionsContext.Provider value={NO_ACTIONS}>
          <SessionMenuItems kit={kit} session={session("s")} pinned={false} subagents={subagents} />
        </SessionActionsContext.Provider>
      </QueryClientProvider>,
    );

  it("a labelled Subagents group, one item each", () => {
    const html = menu(agents);
    expect(html).toContain("<p data-label=\"\">Subagents</p>");
    expect(html).toContain('data-detail="running">Review</button>');
    expect(html).toContain('data-detail="done">Docs</button>');
  });

  it("no group for a session with none", () => {
    expect(menu([])).not.toContain("Subagents");
  });
});

describe("dismissing a failure from Needs you", () => {
  const failed = (id: string, over: Partial<AgentSession> = {}) =>
    session(id, { status: "error", preview: "Stopped.", updatedAt: 10, ...over });
  const needsIds = (html: string) =>
    cardIds(html.slice(html.indexOf('data-testid="agent-needs-you"'), html.indexOf('data-testid="agent-session-group"')));

  it("a failed row offers Dismiss as a button that names itself and is a Tab stop", () => {
    const html = render({ sessions: [failed("a")], onDismissFailure: noop });
    expect(needsIds(html)).toEqual(["a"]);
    const tag = tagOf(html, "agent-card-dismiss");
    expect(tag).toContain('aria-label="Dismiss from Needs you"');
    expect(tag).toContain('type="button"');
    expect(tag).not.toContain('tabindex="-1"');
  });

  it("offers nothing without a handler, for a healthy row and while something waits", () => {
    expect(render({ sessions: [failed("a")] })).not.toContain("agent-card-dismiss");
    const withHandler = { onDismissFailure: noop };
    expect(render({ sessions: [session("a")], ...withHandler })).not.toContain("agent-card-dismiss");
    const html = render({ sessions: [failed("a")], attention: { a: { permissions: 1, questions: 0 } }, ...withHandler });
    expect(needsIds(html)).toEqual(["a"]);
    expect(html).not.toContain("agent-card-dismiss");
  });

  it("a dismissed failure leaves Needs you for its group, quiet, and is not offered again", () => {
    const html = render({ sessions: [failed("a"), failed("b")], dismissed: { a: 10 }, onDismissFailure: noop });
    expect(needsIds(html)).toEqual(["b"]);
    expect(html.indexOf('data-session-id="a"')).toBeGreaterThan(html.indexOf('data-testid="agent-session-group"'));
    const card = cardHtml(html, "a");
    expect(card).toContain('data-dismissed="true"');
    // Quiet: no error mark on the face, and the line is not the alarm colour.
    expect(card).not.toContain("text-status-danger");
    expect(card).not.toContain("agent-card-dismiss");
    expect(card).toContain("Stopped with an error, dismissed");
  });

  it("comes back to Needs you when it fails again", () => {
    const html = render({ sessions: [failed("a", { updatedAt: 11 })], dismissed: { a: 10 } });
    expect(needsIds(html)).toEqual(["a"]);
  });

  it("stops marking the parent, and no Needs you is drawn when it was the only reason", () => {
    const parent = session("p");
    const child = failed("k", { parentSessionId: "p", depth: 1 });
    const marked = render({ sessions: [parent, child] });
    expect(marked).toContain('data-hint="failed"');
    const quiet = render({ sessions: [parent, child], dismissed: { k: 10 } });
    expect(quiet).not.toContain('data-hint="failed"');
    expect(quiet).not.toContain('data-testid="agent-needs-you"');
    expect(cardIds(quiet).sort()).toEqual(["k", "p"]);
  });

  it("the menu offers Dismiss before Close session, only when it applies", () => {
    const kit: MenuKit = {
      Item: ({ children }) => <button type="button">{children}</button>,
      Separator: () => <hr />,
      Label: ({ children }) => <p>{children}</p>,
    };
    const menu = (dismissable: boolean, actions: SessionActions) =>
      renderToString(
        <QueryClientProvider client={new QueryClient()}>
          <SessionActionsContext.Provider value={actions}>
            <SessionMenuItems kit={kit} session={failed("a")} pinned={false} subagents={[]} dismissable={dismissable} />
          </SessionActionsContext.Provider>
        </QueryClientProvider>,
      );
    const actions: SessionActions = { ...NO_ACTIONS, dismissFailure: noop, closeSession: noop };
    const html = menu(true, actions);
    expect(html).toContain("Dismiss from Needs you");
    expect(html.indexOf("Dismiss from Needs you")).toBeLessThan(html.indexOf("Close session"));
    expect(menu(false, actions)).not.toContain("Dismiss from Needs you");
    expect(menu(true, { ...NO_ACTIONS, closeSession: noop })).not.toContain("Dismiss from Needs you");
    // Close is still there, whatever happens to Dismiss.
    expect(menu(false, actions)).toContain("Close session");
  });
});

describe("after needs-you", () => {
  it("a waiting row in a workspace of several folders does not say its folder", () => {
    const html = render({
      sessions: [session("a", { projectPath: "/work/web", workspaceId: "ws1", status: "error", preview: "Broke." })],
      workspaces: [{ id: "ws1", name: "Billing", paths: ["/work/api", "/work/web"] }],
    });
    expect(html).toContain('data-testid="agent-needs-you"');
    expect(cardHtml(html, "a")).toMatch(/agent-card-line[^>]*>Broke\.<\/span>/);
  });

  it("the face's ring colour is the stylesheet's, not an inline style", () => {
    const html = render({ sessions: aged(2), activeSessionId: "s1" });
    expect(html).not.toContain("--avatar-ground");
  });

  it("the session on screen is drawn though its workspace is folded", () => {
    const html = render({
      sessions: [inWs("x", "1"), inWs("y", "2")],
      workspaces: [ws("1"), ws("2")],
      currentWorkspaceId: "1",
      activeSessionId: "y",
    });
    expect(cardIds(html)).toContain("y");
    expect(html).not.toMatch(/data-group-key="workspace:2"[^>]*data-collapsed="true"/);
  });

  it("counts are said in words to a screen reader, and Needs you is named by its label", () => {
    const html = render({ sessions: [...aged(3), session("w", { status: "error" })] });
    expect(html).toContain('<span class="sr-only">, 3 sessions</span>');
    expect(html).toContain('<span class="sr-only">, 1 session</span>');
    const id = html.match(/data-testid="agent-needs-you" aria-labelledby="([^"]+)"/)?.[1];
    expect(id).toBeTruthy();
    expect(html).toContain(`id="${id}"`);
    expect(html).not.toContain('aria-label="Needs you"');
  });

  it("the rail draws its focus rings with the focus token", () => {
    const dir = import.meta.dir;
    const offenders = readdirSync(dir)
      .filter((name) => /\.tsx?$/.test(name) && !/\.test\./.test(name))
      .filter((name) => readFileSync(join(dir, name), "utf8").includes("ring-status-info"));
    expect(offenders).toEqual([]);
  });
});

describe("messages from other sessions on a row", () => {
  it("marks the row with how many arrived, and says it in words", () => {
    const html = renderCard({ session: session("s1"), peerUnread: 2 });
    expect(html).toContain('data-testid="agent-card-peer-unread"');
    expect(html).toContain('aria-label="2 new messages from other sessions"');
    expect(renderCard({ session: session("s1"), peerUnread: 1 })).toContain('aria-label="1 new message from another session"');
    // The tooltip and the description carry it too, for a screen reader.
    expect(html).toContain("2 new messages from other sessions");
  });

  it("draws nothing for a session with none", () => {
    expect(renderCard({ session: session("s1") })).not.toContain("agent-card-peer-unread");
    expect(renderCard({ session: session("s1"), peerUnread: 0 })).not.toContain("agent-card-peer-unread");
  });

  it("stays out of Needs you: nothing waits on the person", () => {
    const html = render({ sessions: [session("s1"), session("s2")], peerUnread: { s1: 3 } });
    expect(html).not.toContain('data-testid="agent-needs-you"');
    // Still in its own group, marked.
    expect(cardHtml(html, "s1")).toContain("agent-card-peer-unread");
    expect(cardHtml(html, "s2")).not.toContain("agent-card-peer-unread");
    // And it neither takes the waiting dot nor the waiting state.
    expect(cardHtml(html, "s1")).not.toContain("data-waiting");
  });
});

describe("children under their parent", () => {
  const family = () => [
    session("root", { updatedAt: 50, status: "idle" }),
    session("a", { parentSessionId: "root", depth: 1, updatedAt: 40 }),
    session("b", { parentSessionId: "root", depth: 1, updatedAt: 30 }),
    session("b1", { parentSessionId: "b", depth: 2, updatedAt: 20 }),
  ];
  /** The part of the page from a node's opening to the next node's: its card and what it holds. */
  const nodeHtml = (html: string, id: string) => {
    const at = html.lastIndexOf('<div role="listitem"', html.indexOf(`data-session-id="${id}"`));
    return html.slice(at);
  };

  it("draws a child inside its parent's list item, in a list of its own, in tree order", () => {
    const html = render({ sessions: family() });
    expect(cardIds(html)).toEqual(["root", "a", "b", "b1"]);
    const root = nodeHtml(html, "root");
    expect(root.indexOf('data-testid="agent-branch-list"')).toBeGreaterThan(root.indexOf('data-session-id="root"'));
    expect(root.indexOf('data-session-id="a"')).toBeGreaterThan(root.indexOf('data-testid="agent-branch-list"'));
    // Real levels, whatever is indented.
    expect(html).toMatch(/role="listitem" aria-level="1"[^>]*data-level="0"/);
    expect(html).toMatch(/role="listitem" aria-level="2"[^>]*data-level="1"/);
    expect(html).toMatch(/role="listitem" aria-level="3"[^>]*data-level="2"/);
    // Lists and list items, not a tree widget: the cards hold buttons and menus.
    expect(html).not.toContain('role="tree"');
  });

  it("a parent says how many sessions are under it, and a child carries the branch mark", () => {
    const html = render({ sessions: family() });
    expect(cardHtml(html, "root")).toMatch(/data-testid="agent-branch-count">3</);
    expect(cardHtml(html, "b")).toMatch(/data-testid="agent-branch-count">1</);
    expect(cardHtml(html, "a")).not.toContain("agent-branch-toggle");
    expect(cardHtml(html, "a")).toContain("agent-nested-mark");
    expect(cardHtml(html, "root")).not.toContain("agent-nested-mark");
  });

  it("the toggle is a button that says what it does and whether it is open", () => {
    const html = render({ sessions: family() });
    const toggle = tagOf(html, "agent-branch-toggle");
    expect(toggle).toContain('aria-expanded="true"');
    expect(toggle).toContain('aria-label="Hide 3 child sessions"');
    expect(toggle).toMatch(/aria-controls="[^"]+"/);
    expect(buttonDepth(html)).toBe(1);
  });

  it("indents children and joins each to its parent with an elbow; the last has no line going on", () => {
    const html = render({ sessions: family() });
    expect(html).toContain("padding-left:28px");
    // One guide per child: a, b, b1.
    expect(html.match(/data-testid="agent-branch-guide"/g)).toHaveLength(3);
    // a is not last: line + elbow. b is last under root: elbow only. b1: elbow only.
    const guide = (id: string) => nodeHtml(html, id).slice(nodeHtml(html, id).indexOf('data-testid="agent-branch-guide"')).split("</span></span>")[0];
    expect(guide("a").match(/border-left/g)?.length).toBe(2);
    expect(guide("b").match(/border-left/g)?.length).toBe(1);
  });

  it("stops indenting after two levels but keeps the real level", () => {
    const deep = [
      session("r", { updatedAt: 9 }),
      session("l1", { parentSessionId: "r", depth: 1, updatedAt: 8 }),
      session("l2", { parentSessionId: "l1", depth: 2, updatedAt: 7 }),
      session("l3", { parentSessionId: "l2", depth: 3, updatedAt: 6 }),
      session("l4", { parentSessionId: "l3", depth: 4, updatedAt: 5 }),
    ];
    const html = render({ sessions: deep });
    expect(cardIds(html)).toEqual(["r", "l1", "l2", "l3", "l4"]);
    // r → l1 and l1 → l2 are indented; l2 → l3 and l3 → l4 are not.
    expect(html.match(/padding-left:28px/g)).toHaveLength(2);
    expect(html.match(/data-testid="agent-branch-guide"/g)).toHaveLength(2);
    expect(html).toMatch(/aria-level="5"[^>]*data-level="4"/);
  });

  it("a folded branch draws no children, and says so", () => {
    const html = render({ sessions: family(), foldedBranchIds: ["root"] });
    expect(cardIds(html)).toEqual(["root"]);
    expect(tagOf(html, "agent-branch-toggle")).toContain('aria-expanded="false"');
    expect(tagOf(html, "agent-branch-toggle")).toContain('aria-label="Show 3 child sessions"');
    expect(tagOf(html, "agent-branch-toggle")).not.toContain("aria-controls");
  });

  it("folding one branch leaves its siblings and the rest of the tree alone", () => {
    const html = render({ sessions: family(), foldedBranchIds: ["b"] });
    expect(cardIds(html)).toEqual(["root", "a", "b"]);
  });

  it("the session on screen holds its branch open, though it was folded, and the toggle cannot fold it", () => {
    const html = render({ sessions: family(), foldedBranchIds: ["root", "b"], activeSessionId: "b1" });
    expect(cardIds(html)).toEqual(["root", "a", "b", "b1"]);
    expect(cardHtml(html, "root")).toContain("data-held");
    expect(cardHtml(html, "root")).toContain('aria-disabled="true"');
    expect(cardHtml(html, "a")).not.toContain("data-held");
  });

  it("a pinned child holds its branch open too", () => {
    const html = render({ sessions: family(), foldedBranchIds: ["root"], pinnedIds: ["a"] });
    expect(cardIds(html)).toEqual(["root", "a", "b", "b1"]);
  });

  it("a folded parent carries one mark for what runs below it, and an open one none", () => {
    const sessions = [session("root", { updatedAt: 5 }), session("run", { parentSessionId: "root", depth: 1, status: "running" })];
    const folded = render({ sessions, foldedBranchIds: ["root"] });
    expect(cardHtml(folded, "root")).toMatch(/data-testid="agent-branch-hint" data-hint="running"/);
    expect(cardHtml(folded, "root")).toContain("A child session is running");
    expect(render({ sessions })).not.toContain("agent-branch-hint");
  });

  it("a folded parent carries a mark for unread messages below it", () => {
    const sessions = [session("root", { updatedAt: 5 }), session("kid", { parentSessionId: "root", depth: 1 })];
    const html = render({ sessions, foldedBranchIds: ["root"], peerUnread: { kid: 2 } });
    expect(cardHtml(html, "root")).toMatch(/data-hint="unread"/);
  });

  it("a child waiting on the person is drawn in Needs you, and its parent is marked, open or not", () => {
    const sessions = [
      session("root", { updatedAt: 5 }),
      session("ok", { parentSessionId: "root", depth: 1, updatedAt: 4 }),
      session("ask", { parentSessionId: "root", depth: 1, updatedAt: 3, status: "awaiting_permission" }),
    ];
    for (const foldedBranchIds of [[], ["root"]]) {
      const html = render({ sessions, foldedBranchIds });
      expect(html).toContain('data-testid="agent-needs-you"');
      expect(cardHtml(html, "root")).toMatch(/data-hint="waiting"/);
      expect(cardHtml(html, "root")).toContain("A child session is waiting for you");
    }
    // Drawn once: in Needs you, not also under its parent.
    expect(cardIds(render({ sessions })).filter((id) => id === "ask")).toHaveLength(1);
    // The count is what is drawn under it.
    expect(cardHtml(render({ sessions }), "root")).toMatch(/data-testid="agent-branch-count">1</);
  });

  it("a failed child is marked on its parent as well", () => {
    const sessions = [session("root", { updatedAt: 5 }), session("bad", { parentSessionId: "root", depth: 1, status: "error" })];
    const html = render({ sessions });
    // Nothing is drawn under it, so the mark stands alone, with no fold.
    expect(cardHtml(html, "root")).toMatch(/data-hint="failed"/);
    expect(cardHtml(html, "root")).toContain("agent-branch-mark");
    expect(cardHtml(html, "root")).not.toContain("agent-branch-toggle");
  });

  it("a tree is one of the five recent: six trees fold only one", () => {
    const sessions = [
      ...aged(6),
      session("k1", { parentSessionId: "s0", depth: 1, updatedAt: 1 }),
      session("k2", { parentSessionId: "s0", depth: 1, updatedAt: 1 }),
      session("k3", { parentSessionId: "s1", depth: 1, updatedAt: 1 }),
    ];
    const html = render({ sessions });
    expect(html).toContain("1 older session");
    expect(cardIds(html)).toEqual(["s0", "k1", "k2", "s1", "k3", "s2", "s3", "s4"]);
  });

  it("a child on screen keeps its old tree out of the stack", () => {
    const sessions = [
      ...aged(7),
      session("deep", { parentSessionId: "s6", depth: 1, updatedAt: 1 }),
    ];
    const html = render({ sessions, activeSessionId: "deep" });
    expect(cardIds(html)).toContain("deep");
    expect(cardIds(html)).toContain("s6");
    expect(html).toContain("1 older session");
  });

  it("a child whose parent is gone, or hidden as closed, is drawn as a root and keeps its group", () => {
    const orphan = render({ sessions: [session("kid", { parentSessionId: "deleted", depth: 1 })] });
    expect(cardIds(orphan)).toEqual(["kid"]);
    expect(orphan).not.toContain("agent-branch-list");
    const hidden = render({
      sessions: [session("p", { status: "closed" }), session("kid", { parentSessionId: "p", depth: 1 })],
      hideClosed: true,
    });
    expect(cardIds(hidden)).toEqual(["kid"]);
    expect(hidden).not.toContain("agent-branch-toggle");
  });

  it("data with a cycle still draws every session once", () => {
    const html = render({
      sessions: [
        session("a", { parentSessionId: "b", updatedAt: 1 }),
        session("b", { parentSessionId: "a", updatedAt: 2 }),
        session("self", { parentSessionId: "self", updatedAt: 3 }),
      ],
    });
    expect(cardIds(html).sort()).toEqual(["a", "b", "self"]);
  });

  it("a child stays in its parent's group, drawn under it", () => {
    const html = render({
      sessions: [
        inWs("p", "1", { updatedAt: 2 }),
        session("kid", { parentSessionId: "p", depth: 1, projectPath: "/elsewhere", workspaceId: "", updatedAt: 1 }),
      ],
      workspaces: [ws("1")],
    });
    expect(html).not.toContain('data-group-key="project:/elsewhere"');
    expect(cardIds(html)).toEqual(["p", "kid"]);
    expect(nodeHtml(html, "p")).toContain('data-session-id="kid"');
  });

  it("folding is remembered per session in the store, and persists", () => {
    const { toggleBranchFolded } = useAgentPinsStore.getState();
    toggleBranchFolded("root");
    toggleBranchFolded("other");
    expect(useAgentPinsStore.getState().foldedBranches).toEqual(["root", "other"]);
    toggleBranchFolded("root");
    expect(useAgentPinsStore.getState().foldedBranches).toEqual(["other"]);
    toggleBranchFolded("other");
  });

  it("a row in Needs you keeps the list item of its own", () => {
    const html = render({ sessions: [session("w", { status: "awaiting_permission" })] });
    expect(html).toMatch(/<div role="listitem" data-slot="agent-session-card"/);
  });
});

describe("a branch with many children", () => {
  /** A root and n idle children, k0 the newest. */
  const family = (n: number, over: (i: number) => Partial<AgentSession> = () => ({})) => [
    session("root", { updatedAt: 1000 }),
    ...Array.from({ length: n }, (_, i) =>
      session(`k${i}`, { parentSessionId: "root", depth: 1, updatedAt: 100 - i, ...over(i) }),
    ),
  ];
  const ctx = (over: Partial<BranchContext> = {}): BranchContext => ({
    pinned: new Set(),
    folded: new Set(),
    held: new Set(),
    subagents: {},
    peerUnread: {},
    signals: {},
    titles: new Map(),
    now: Date.now(),
    ...over,
  });
  /** One tree on its own, so the state a press would set can be given up front. */
  function renderBranch(sessions: AgentSession[], over: Partial<BranchContext> = {}): string {
    const [root] = buildForest(sessions);
    return renderToString(
      <TooltipProvider>
        <SessionActionsContext.Provider value={NO_ACTIONS}>
          <SessionBranch node={root} first={false} last={false} guided={false} ctx={ctx(over)} />
        </SessionActionsContext.Provider>
      </TooltipProvider>,
    );
  }

  it("draws three of eleven idle children and a row that says how many are folded", () => {
    const html = render({ sessions: family(11) });
    expect(cardIds(html)).toEqual(["root", "k0", "k1", "k2"]);
    const more = tagOf(html, "agent-branch-more");
    expect(more).toContain('aria-expanded="false"');
    expect(more).toContain('aria-label="Show 8 more child sessions"');
    expect(more).toMatch(/aria-controls="[^"]+"/);
    expect(html).toContain("+8 more");
    // The parent still counts every child it started.
    expect(cardHtml(html, "root")).toMatch(/data-testid="agent-branch-count">11</);
    expect(buttonDepth(html)).toBe(1);
  });

  it("draws every running child, and no row when nothing is folded", () => {
    const running = family(8, (i) => ({ status: i >= 2 && i < 7 ? "running" : "idle" }));
    const html = render({ sessions: running });
    expect(cardIds(html)).toEqual(["root", "k2", "k3", "k4", "k5", "k6"]);
    expect(html).toContain("agent-branch-more");
    expect(render({ sessions: family(3) })).not.toContain("agent-branch-more");
  });

  it("draws the session on screen though it is old", () => {
    const html = render({ sessions: family(11), activeSessionId: "k9" });
    expect(cardIds(html)).toEqual(["root", "k0", "k1", "k2", "k9"]);
    expect(tagOf(html, "agent-branch-more")).toContain('aria-label="Show 7 more child sessions"');
  });

  it("draws all of them, with a way back, once shown", () => {
    const html = renderBranch(family(11), { initialShowAll: new Set(["root"]) });
    expect(cardIds(html)).toHaveLength(12);
    const more = tagOf(html, "agent-branch-more");
    expect(more).toContain('aria-expanded="true"');
    expect(more).toContain('aria-label="Show fewer child sessions"');
    expect(html).toContain("Show fewer");
  });

  it("marks a folded child that has a message the person has not seen", () => {
    const html = render({ sessions: family(11), peerUnread: { k8: 1, k9: 2 } });
    const dot = tagOf(html, "agent-branch-more-unread");
    expect(dot).toContain('aria-label="2 folded sessions have new messages"');
    // An unread child that is drawn says so itself, so it is not counted here.
    expect(tagOf(render({ sessions: family(11), peerUnread: { k0: 1 } }), "agent-branch-more-unread")).toBe("");
  });

  it("ends the guide line at the row, and the row has an elbow like its siblings", () => {
    const html = render({ sessions: family(11) });
    // Three children and the row: four guides; the last child is not last any more.
    expect(html.match(/data-testid="agent-branch-guide"/g)).toHaveLength(4);
    const rowAt = html.indexOf('data-testid="agent-branch-more-row"');
    // The guide follows the row's button inside its list item; nothing after it draws a line.
    const row = html.slice(rowAt);
    expect(row.match(/border-left/g)?.length).toBe(1); // elbow only: the line ends here
    expect(html.slice(html.indexOf('data-session-id="k2"'), rowAt).match(/border-left/g)?.length).toBe(2); // k2: line + elbow
  });

  it("folding the parent hides the row with the children", () => {
    const html = render({ sessions: family(11), foldedBranchIds: ["root"] });
    expect(cardIds(html)).toEqual(["root"]);
    expect(html).not.toContain("agent-branch-more");
  });

  it("applies at every level", () => {
    const sessions = [
      ...family(1),
      ...Array.from({ length: 6 }, (_, i) => session(`g${i}`, { parentSessionId: "k0", depth: 2, updatedAt: 50 - i })),
    ];
    const html = render({ sessions });
    expect(cardIds(html)).toEqual(["root", "k0", "g0", "g1", "g2"]);
    expect(tagOf(html, "agent-branch-more")).toContain('aria-label="Show 3 more child sessions"');
  });
});
