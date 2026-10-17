import { useMemo, useState } from "react";
import type { AgentSession } from "@/bridge/agenthub";
import type { Subagent } from "@/modules/agenthub/types";
import { AgentSessionSidebar } from "@/modules/agenthub/sidebar/AgentSessionSidebar";
import type { Attention } from "@/modules/agenthub/sidebar/model";

/**
 * The real Agent Hub rail (AgentSessionSidebar), fed fixtures: groups of 1, 3,
 * 7 and 26 sessions, long titles, a crowd of subagents, and every state a card
 * can be in, and parents with children drawn under them (a tree is one of the
 * five recent; a folded parent marks what runs, waits or is unread below it).
 * Interactive, so folding, the stack, pinning and selection can be
 * tried without a bridge. `?gallery#agenthub-session-rail`.
 */
const NOW = Date.now();
const ago = (minutes: number) => NOW - minutes * 60_000;

function s(id: string, projectPath: string, title: string, minutes: number, over: Partial<AgentSession> = {}): AgentSession {
  return {
    id,
    agentType: "claude",
    projectPath,
    workspaceId: "",
    status: "idle",
    parentSessionId: null,
    depth: 0,
    title,
    acpSessionId: id,
    updatedAt: ago(minutes),
    ...over,
  };
}

const API = "/work/billing/api";
const WEB = "/work/billing/web";
const DOCS = "/work/docs";
const SCRATCH = "/cfg/agent-scratch";

const manyTitles = [
  "Refactor invoice rounding",
  "Why does the retry test flake on CI?",
  "Add idempotency keys to POST /payments",
  "Migrate the ledger table to partitioned storage and backfill historical rows",
  "Review the OpenAPI diff",
  "Bump Spring Boot to the next minor",
  "Kafka consumer lag investigation",
  "Rewrite README quick start",
  "Fix N+1 in the account loader",
  "Trace the 502s from the gateway",
];

const sessions: AgentSession[] = [
  // Workspace "billing": 26 sessions → five recent and a stack of twenty-one.
  ...Array.from({ length: 26 }, (_, i) =>
    s(`b${i}`, i % 3 ? API : WEB, manyTitles[i % manyTitles.length] + (i >= 10 ? ` (${i})` : ""), 3 + i * 47, {
      workspaceId: "ws-billing",
      agentType: i % 4 === 0 ? "codex" : "claude",
    }),
  ),
  // The states, in the group a person looks at first.
  s("run", API, "Running a long migration", 1, { workspaceId: "ws-billing", status: "running" }),
  s("perm", API, "Wants to run `rm -rf build`", 2, { workspaceId: "ws-billing", status: "awaiting_permission" }),
  s("ask", WEB, "Which pricing tier should the banner show?", 4, { workspaceId: "ws-billing", status: "running", agentType: "codex" }),
  s("err", API, "Crashed: vendor process exited", 9, { workspaceId: "ws-billing", status: "error" }),
  s("kid", API, "Child: write the failing test first", 6, { workspaceId: "ws-billing", parentSessionId: "run", depth: 1 }),
  // A parent with four children, one of them with two of its own and one that is
  // waiting (drawn in Needs you, marked on the parent), as the Claude desktop rail.
  s("fam", API, "Codex stopped abruptly in devkit", 2, { workspaceId: "ws-billing", agentType: "codex" }),
  s("fam1", API, "Model/effort for agent-created sessions", 1, { workspaceId: "ws-billing", parentSessionId: "fam", depth: 1, status: "running", agentType: "codex" }),
  s("fam2", API, "Cross-session messaging in Agent Hub", 3, { workspaceId: "ws-billing", parentSessionId: "fam", depth: 1 }),
  s("fam3", API, "PR status tracking in the Agent Hub rail", 5, { workspaceId: "ws-billing", parentSessionId: "fam", depth: 1 }),
  s("fam3a", API, "Poll the forge, conditional reads", 4, { workspaceId: "ws-billing", parentSessionId: "fam3", depth: 2, status: "running" }),
  s("fam3b", API, "Write the card for a failing check", 6, { workspaceId: "ws-billing", parentSessionId: "fam3", depth: 2 }),
  s("fam3c", API, "Level three, still indented", 7, { workspaceId: "ws-billing", parentSessionId: "fam3b", depth: 3 }),
  s("fam3d", API, "Level four keeps the indent of level three, and its title is long enough to truncate", 8, { workspaceId: "ws-billing", parentSessionId: "fam3c", depth: 4 }),
  s("fam4", API, "UI viewer for Markdown and PDF", 9, { workspaceId: "ws-billing", parentSessionId: "fam", depth: 1 }),
  s("famw", API, "Wants to run `git push` for the rail work", 10, { workspaceId: "ws-billing", parentSessionId: "fam", depth: 1, status: "awaiting_permission" }),
  // A second tree, folded in the "folded" rail: running and unread children show on the parent.
  s("lone", WEB, "Review the checkout redesign", 12, { workspaceId: "ws-billing" }),
  s("lone1", WEB, "Check the empty states", 11, { workspaceId: "ws-billing", parentSessionId: "lone", depth: 1, status: "running" }),
  s("lone2", WEB, "Check keyboard order", 13, { workspaceId: "ws-billing", parentSessionId: "lone", depth: 1 }),
  // A child whose parent is not in the list is drawn as a root.
  s("orphan", DOCS, "Child of a session that was deleted", 40, { parentSessionId: "deleted", depth: 1 }),
  // An old session that is waiting must show even though it is far down.
  s("old-wait", API, "Old session that still needs an answer", 60 * 24 * 9, { workspaceId: "ws-billing", status: "awaiting_permission" }),
  // A folder with one session, one with three, and the scratch group.
  s("d1", DOCS, "Docs sweep", 30),
  s("w1", "/work/solo", "A single session in its own folder, with a title long enough to need truncating", 120),
  s("c1", "/work/closed-only", "Closed yesterday", 60 * 20, { status: "closed" }),
  s("n1", SCRATCH + "/20261001-1-a", "Scratch idea", 600),
  s("n2", SCRATCH + "/20261002-2-b", "Another scratch idea", 900),
];

const subagents: Record<string, Subagent[]> = {
  run: [
    { id: "r1", name: "Review whitelist", task: "Read the allow list and report gaps" },
    { id: "r2", name: "Java fixes", task: "Patch the failing tests" },
    { id: "r3", name: "Docs", state: "completed" },
    { id: "r4", name: "Flaky test hunt", state: "failed" },
    { id: "r5", task: "Anonymous helper" },
  ],
  b0: [{ id: "x1", name: "Explore" }],
  b1: [{ id: "x2", name: "Planner" }, { id: "x3", name: "Writer", state: "completed" }],
};

const attention: Record<string, Attention> = {
  perm: { permissions: 1, questions: 0 },
  ask: { permissions: 0, questions: 2 },
  "old-wait": { permissions: 1, questions: 0 },
  famw: { permissions: 1, questions: 0 },
};

const peerUnread: Record<string, number> = { fam2: 2, lone2: 1 };

const workspaces = [{ id: "ws-billing", name: "billing-platform", paths: [API, WEB] }];

/** `collapsed` / `older` start as given, so a capture can open on any of them. */
function Rail({ width, extraAttention, initialCollapsed = [] as string[], initialOlder = [] as string[], initialFolded = [] as string[], initialActive = "b2" }: {
  width: number;
  /** Merged over the fixture's own: lets a capture make one more session start waiting. */
  extraAttention?: Record<string, Attention>;
  initialCollapsed?: string[];
  initialOlder?: string[];
  /** Sessions whose children start folded. */
  initialFolded?: string[];
  initialActive?: string;
}) {
  const [active, setActive] = useState(initialActive);
  const [collapsed, setCollapsed] = useState(initialCollapsed);
  const [older, setOlder] = useState(initialOlder);
  const [folded, setFolded] = useState(initialFolded);
  const [pinned, setPinned] = useState<string[]>(["b11"]);
  const [hideClosed, setHideClosed] = useState(false);
  const toggle = (list: string[], set: (v: string[]) => void) => (key: string) =>
    set(list.includes(key) ? list.filter((k) => k !== key) : [...list, key]);
  const items = useMemo(() => sessions, []);
  const waiting = useMemo(() => ({ ...attention, ...extraAttention }), [extraAttention]);
  return (
    <div style={{ width }} className="h-160 overflow-hidden rounded-control border border-border-subtle bg-(--background-app)">
      <AgentSessionSidebar
        sessions={items}
        workspaces={workspaces}
        scratchRoot={SCRATCH}
        activeSessionId={active}
        attention={waiting}
        subagents={subagents}
        peerUnread={peerUnread}
        foldedBranchIds={folded}
        onToggleBranch={toggle(folded, setFolded)}
        pinnedIds={pinned}
        onTogglePin={(id) => toggle(pinned, setPinned)(id)}
        collapsedKeys={collapsed}
        onToggleCollapsed={toggle(collapsed, setCollapsed)}
        olderOpenKeys={older}
        onToggleOlder={toggle(older, setOlder)}
        hideClosed={hideClosed}
        onToggleHideClosed={() => setHideClosed((v) => !v)}
        onSelectSession={setActive}
        onSelectSubagent={(session) => setActive(session.id)}
        onNewSession={() => {}}
        onNewSessionIn={() => {}}
        onNewChildSession={() => {}}
        onRenameSession={() => {}}
        onCopyTranscript={() => {}}
        onCopy={() => {}}
        onCancelTurn={() => {}}
        onCloseSession={() => {}}
        onDeleteSession={() => {}}
      />
    </div>
  );
}

export function AgentSessionSidebarSection() {
  const [asked, setAsked] = useState<Record<string, Attention> | undefined>();
  return (
    <section id="agenthub-session-rail" className="flex flex-col gap-4 p-6">
      <h2 className="text-title-16 text-text-primary">Agent Hub — session rail</h2>
      <div className="flex gap-2">
        {/* b0 is in the five recent; b20 is in the stack. Either starts waiting. */}
        <button type="button" data-testid="gallery-wait-b0" className="rounded-md border border-border-subtle px-2 py-1 text-caption-11 text-text-secondary" onClick={() => setAsked({ b0: { permissions: 1, questions: 0 } })}>
          Make a recent session ask permission
        </button>
        <button type="button" data-testid="gallery-wait-b20" className="rounded-md border border-border-subtle px-2 py-1 text-caption-11 text-text-secondary" onClick={() => setAsked({ b20: { permissions: 0, questions: 1 } })}>
          Make a stacked session ask a question
        </button>
      </div>
      <div className="flex flex-wrap items-start gap-6">
        <div data-testid="rail-default"><Rail width={280} extraAttention={asked} /></div>
        <div data-testid="rail-narrow"><Rail width={240} initialActive="fam3b" /></div>
        <div data-testid="rail-wide"><Rail width={360} initialOlder={["workspace:ws-billing"]} initialFolded={["fam3"]} /></div>
        <div data-testid="rail-branches-folded"><Rail width={280} initialFolded={["fam", "lone"]} initialActive="run" /></div>
        <div data-testid="rail-folded"><Rail width={280} initialCollapsed={["workspace:ws-billing", "project:/work/solo"]} initialActive="d1" /></div>
      </div>
    </section>
  );
}
