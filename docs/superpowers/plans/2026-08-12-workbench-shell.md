# Workbench Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the unstyled `ActivityBar`/`Workbench` shell with a `DESIGN.md`-token shell built from shadcn's `Sidebar`/`Command` primitives — a fixed, icon-heavy, collapsible sidebar (accordion sub-tree + status dot/badge for Agent Hub, visible even when the sidebar is icon-collapsed) plus a floating rounded content card that hosts whichever module is active.

**Architecture:** Lift Agent Hub's session list + status out of `AgentHubView` (which only mounts while that module is active) into a small Zustand store that lives for the app's lifetime, so the sidebar can render live status regardless of which module the user is looking at. Rebuild the shell chrome on top of `components/ui/sidebar.tsx` (`variant="inset"` gives the floating-card effect for free) and `components/ui/command.tsx` instead of hand-rolled CSS, remapping the shadcn theme CSS variables to `DESIGN.md`'s dark palette as the single `:root` (no light mode, no `.dark` toggle exists in the app).

**Tech Stack:** React 19, TypeScript, Tailwind v4, shadcn/ui (`sidebar`, `command`, `collapsible`, `tooltip`, `alert`, `button`, `kbd`), Zustand 5, lucide-react.

---

## Reference

Design spec: `docs/superpowers/specs/2026-08-12-workbench-shell-design.md`

No frontend test runner exists (`frontend/package.json` has no `test` script, no vitest/jest). Every task's automated check is a TypeScript compile:

```bash
cd frontend && bunx tsc --noEmit -p tsconfig.json
```

Expected on success: no output, exit code 0. The final task is a manual dev-server walkthrough — there is no way to automate UI verification here without introducing test infrastructure the spec explicitly decided not to add (YAGNI for this pass).

---

### Task 1: Agent session store

**Files:**
- Create: `frontend/src/stores/agentSessions.ts`

- [ ] **Step 1: Write the store**

```ts
// frontend/src/stores/agentSessions.ts
import { create } from "zustand";
import type { AgentSession } from "../bridge/agenthub";

type AgentSessionsState = {
  sessions: AgentSession[];
  activeSessionId: string;
  setSessions: (sessions: AgentSession[]) => void;
  upsertSession: (session: AgentSession) => void;
  removeSession: (id: string) => void;
  setActiveSessionId: (id: string) => void;
};

export const useAgentSessionsStore = create<AgentSessionsState>((set) => ({
  sessions: [],
  activeSessionId: "",
  setSessions: (sessions) => set({ sessions }),
  upsertSession: (session) =>
    set((state) => {
      const index = state.sessions.findIndex((s) => s.id === session.id);
      if (index === -1) {
        return { sessions: [...state.sessions, session] };
      }
      const next = state.sessions.slice();
      next[index] = session;
      return { sessions: next };
    }),
  removeSession: (id) =>
    set((state) => ({
      sessions: state.sessions.filter((s) => s.id !== id),
    })),
  setActiveSessionId: (id) => set({ activeSessionId: id }),
}));
```

This is the single source of truth for "what Agent Hub sessions exist and what
state are they in" — read by both `AppSidebar` (badge/sub-tree/tooltip, Task
6) and `AgentHubView` (Task 3), so status set from either place is visible in
both without either needing to be mounted.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/agentSessions.ts
git commit -m "feat(agenthub): add shared session store for cross-module status"
```

---

### Task 2: Agent session subscriber

**Files:**
- Create: `frontend/src/workbench/AgentSessionsSubscriber.tsx`

- [ ] **Step 1: Write the subscriber**

```tsx
// frontend/src/workbench/AgentSessionsSubscriber.tsx
import { useEffect } from "react";
import { agentHubListAgents, onAgentHubPermission } from "../bridge/agenthub";
import { useAgentSessionsStore } from "../stores/agentSessions";

/**
 * Keeps the shared Agent Hub session store in sync regardless of which
 * module is mounted. Rendered once in Workbench.tsx, as a sibling of the
 * sidebar and content card — never remounted on module switch, unlike
 * AgentHubView which only exists while that module is active.
 */
export function AgentSessionsSubscriber() {
  const setSessions = useAgentSessionsStore((state) => state.setSessions);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);

  useEffect(() => {
    let mounted = true;
    agentHubListAgents()
      .then((sessions) => {
        if (mounted) {
          setSessions(sessions.filter((s) => s.status !== "closed"));
        }
      })
      .catch(() => {
        // No sessions yet, or the hub is unavailable — sidebar just shows
        // no Agent Hub badge, same as an empty session list.
      });
    return () => {
      mounted = false;
    };
  }, [setSessions]);

  useEffect(() => {
    return onAgentHubPermission((pending) => {
      const session = useAgentSessionsStore
        .getState()
        .sessions.find((s) => s.id === pending.sessionId);
      if (session) {
        upsertSession({ ...session, status: "awaiting_permission" });
      }
    });
  }, [upsertSession]);

  return null;
}
```

Note: this subscriber does **not** listen to `onAgentHubSessionUpdate`
(transcript text chunks) — that event never carries a status field (see
`bridge/agenthub.ts`'s `SessionUpdateEvent` type), only chat text, and
transcript stays local to `AgentHubView` per the spec (Task 3). Only
`onAgentHubPermission` carries information the sidebar needs to know about
regardless of which module is mounted.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/AgentSessionsSubscriber.tsx
git commit -m "feat(agenthub): subscribe permission events into the shared session store"
```

---

### Task 3: Lift AgentHubView onto the shared store

**Files:**
- Modify: `frontend/src/modules/agenthub/AgentHubView.tsx` (full rewrite)

- [ ] **Step 1: Replace the file contents**

```tsx
// frontend/src/modules/agenthub/AgentHubView.tsx
import { useEffect, useMemo, useState } from "react";
import {
  agentHubCreateSession,
  agentHubGetStatus,
  agentHubListVendorTypes,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  isSandboxUnavailableError,
  onAgentHubPermission,
  onAgentHubSessionUpdate,
  parsePermissionOptions,
  permissionToolTitle,
  sandboxUnavailableMessage,
  selectedPermissionOutcome,
  type AgentVendor,
  type PendingPermission,
} from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { appendUpdateLine, SessionTab, type TranscriptLine } from "./SessionTab";
import { useAgentSessionsStore } from "../../stores/agentSessions";

type TranscriptState = {
  lines: TranscriptLine[];
  prompt: string;
};

const DEFAULT_VENDORS: AgentVendor[] = ["mock", "claude"];
const EMPTY_TRANSCRIPT: TranscriptState = { lines: [], prompt: "" };

export function AgentHubView() {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  const [vendor, setVendor] = useState<string>("mock");
  const [cwd, setCwd] = useState(".");
  const [transcripts, setTranscripts] = useState<Record<string, TranscriptState>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sandboxHint, setSandboxHint] = useState("");
  const [pending, setPending] = useState<PendingPermission | null>(null);
  const [permBusy, setPermBusy] = useState(false);

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? sessions[0],
    [sessions, activeSessionId],
  );
  const activeTranscript = activeSession
    ? transcripts[activeSession.id] ?? EMPTY_TRANSCRIPT
    : EMPTY_TRANSCRIPT;

  useEffect(() => {
    if (!activeSessionId && sessions.length > 0) {
      setActiveSessionId(sessions[0].id);
    }
  }, [sessions, activeSessionId, setActiveSessionId]);

  useEffect(() => {
    let mounted = true;
    agentHubListVendorTypes()
      .then((list) => {
        if (!mounted || list.length === 0) {
          return;
        }
        setVendors(list);
        if (!list.includes(vendor)) {
          setVendor(list[0]);
        }
      })
      .catch(() => {
        // Keep mock|claude defaults when listing fails (e.g. hub unavailable).
      });
    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    const offUpdate = onAgentHubSessionUpdate((event) => {
      setTranscripts((prev) => {
        const current = prev[event.sessionId] ?? EMPTY_TRANSCRIPT;
        return {
          ...prev,
          [event.sessionId]: {
            ...current,
            lines: appendUpdateLine(current.lines, event.update),
          },
        };
      });
    });
    const offPermission = onAgentHubPermission((next) => {
      setPending(next);
    });
    return () => {
      offUpdate();
      offPermission();
    };
  }, []);

  async function onNewTab() {
    setBusy(true);
    setError("");
    setSandboxHint("");
    try {
      const session = await agentHubCreateSession({
        agentType: vendor,
        cwd: cwd.trim() || ".",
      });
      upsertSession(session);
      setActiveSessionId(session.id);
    } catch (e) {
      setError(errorMessage(e));
      if (vendor === "claude" && isSandboxUnavailableError(e)) {
        setSandboxHint(sandboxUnavailableMessage());
      }
    } finally {
      setBusy(false);
    }
  }

  async function resolvePermission(optionId: string | null) {
    if (!pending) {
      return;
    }
    setPermBusy(true);
    setError("");
    try {
      const outcome =
        optionId === null
          ? cancelledPermissionOutcome()
          : selectedPermissionOutcome(optionId);
      await agentHubResolvePermission(pending.id, outcome);
      const refreshed = await agentHubGetStatus(pending.sessionId);
      upsertSession(refreshed);
      setPending(null);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPermBusy(false);
    }
  }

  const permissionOptions = pending
    ? parsePermissionOptions(pending.params)
    : [];

  return (
    <section className="agenthub-view">
      <h2>Agent Hub</h2>

      <div className="agenthub-header">
        <label>
          Vendor
          <select
            aria-label="agenthub-vendor"
            value={vendor}
            disabled={busy}
            onChange={(e) => setVendor(e.target.value)}
          >
            {vendors.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label className="agenthub-cwd">
          Working directory
          <input
            aria-label="agenthub-cwd"
            value={cwd}
            disabled={busy}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="."
          />
        </label>
        <button type="button" onClick={() => void onNewTab()} disabled={busy}>
          New tab
        </button>
      </div>

      {activeSession ? (
        <SessionTab
          session={activeSession}
          lines={activeTranscript.lines}
          prompt={activeTranscript.prompt}
          busy={busy}
          onPromptChange={(prompt) =>
            setTranscripts((prev) => ({
              ...prev,
              [activeSession.id]: { ...(prev[activeSession.id] ?? EMPTY_TRANSCRIPT), prompt },
            }))
          }
          onLinesChange={(updater) =>
            setTranscripts((prev) => ({
              ...prev,
              [activeSession.id]: {
                ...(prev[activeSession.id] ?? EMPTY_TRANSCRIPT),
                lines: updater(prev[activeSession.id]?.lines ?? []),
              },
            }))
          }
          onSessionChange={(session) => upsertSession(session)}
          onError={setError}
        />
      ) : (
        <p className="agenthub-empty">No sessions. Create a tab to begin.</p>
      )}

      {sandboxHint ? (
        <div className="agenthub-sandbox-error" role="alert">
          <p>{sandboxHint}</p>
          <button
            type="button"
            onClick={() => {
              setVendor("mock");
              setSandboxHint("");
              setError("");
            }}
          >
            Switch to mock vendor
          </button>
        </div>
      ) : null}

      {error ? <p role="alert">{error}</p> : null}

      {pending ? (
        <div className="agenthub-permission-backdrop" role="presentation">
          <div
            className="agenthub-permission-modal"
            role="dialog"
            aria-modal="true"
            aria-label="agenthub-permission"
          >
            <h3>{permissionToolTitle(pending.params)}</h3>
            <p>
              Session <code>{pending.sessionId.slice(0, 8)}</code> needs a
              permission decision.
            </p>
            <pre className="agenthub-permission-params">
              {safeJson(pending.params)}
            </pre>
            <div className="agenthub-permission-actions">
              <button
                type="button"
                disabled={permBusy}
                onClick={() => void resolvePermission(null)}
              >
                Deny
              </button>
              {permissionOptions.map((opt) => (
                <button
                  key={opt.optionId}
                  type="button"
                  disabled={permBusy}
                  onClick={() => void resolvePermission(opt.optionId)}
                >
                  {opt.name}
                </button>
              ))}
              {permissionOptions.length === 0 ? (
                <button
                  type="button"
                  disabled={permBusy}
                  onClick={() => void resolvePermission("allow")}
                >
                  Allow
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function safeJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
```

Key changes from the previous version:
- `tabs`/`activeId` (local `useState`) are gone. Session list + which one is
  active now come from `useAgentSessionsStore`; only the transcript
  (`lines`/`prompt`, keyed by session id) stays local to this view.
- The horizontal tab-strip (`<div className="agenthub-tabstrip">`) is
  removed — the sidebar's Agent Hub sub-tree (Task 6) is now the only tab
  switcher, so keeping a second one here would duplicate the same control
  (per the spec's explicit decision).
- `onNewTab` calls `upsertSession` instead of appending to local state, so a
  session created here is immediately visible to the sidebar.
- `resolvePermission` now re-fetches status via `agentHubGetStatus` and
  writes it back with `upsertSession` after resolving — the old version left
  `session.status` stale after a permission decision; this was a real gap in
  the pre-lift code, not a regression introduced here, and it directly
  serves the "status must be correct in the sidebar" requirement.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors. (`agentHubListAgents` and the `AgentSession`/`TabState`
type are no longer imported/defined in this file — if the compiler reports
an unused import, double check nothing from the old version was left in.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/agenthub/AgentHubView.tsx
git commit -m "refactor(agenthub): read session list from the shared store, drop tab-strip"
```

---

### Task 4: Dark-only DESIGN.md theme tokens

**Files:**
- Modify: `frontend/src/index.css:52-119`

- [ ] **Step 1: Replace `:root` and remove the `.dark` block**

The app has no theme toggle (`next-themes` is an unused transitive import in
`components/ui/sonner.tsx` only — no `ThemeProvider` exists anywhere), and
`DESIGN.md` is explicit: "Don't introduce a light mode. The system is
dark-only by design." So the dark palette becomes the only `:root` — replace
lines 52-119 of `frontend/src/index.css` (the `:root { ... }` block through
the end of the `.dark { ... }` block) with:

```css
:root {
    --background: #07080a;
    --foreground: #f4f4f6;
    --card: #0d0d0d;
    --card-foreground: #f4f4f6;
    --popover: #101111;
    --popover-foreground: #f4f4f6;
    --primary: #ffffff;
    --primary-foreground: #000000;
    --secondary: #101111;
    --secondary-foreground: #f4f4f6;
    --muted: #121212;
    --muted-foreground: #9c9c9d;
    --accent: #121212;
    --accent-foreground: #f4f4f6;
    --destructive: #ff6161;
    --border: #242728;
    --input: #242728;
    --ring: #57c1ff;
    --chart-1: oklch(0.87 0 0);
    --chart-2: oklch(0.556 0 0);
    --chart-3: oklch(0.439 0 0);
    --chart-4: oklch(0.371 0 0);
    --chart-5: oklch(0.269 0 0);
    --radius: 0.625rem;
    --sidebar: #07080a;
    --sidebar-foreground: #cdcdcd;
    --sidebar-primary: #ffffff;
    --sidebar-primary-foreground: #000000;
    --sidebar-accent: #121212;
    --sidebar-accent-foreground: #f4f4f6;
    --sidebar-border: #242728;
    --sidebar-ring: #57c1ff;

    /* Agent session status vocabulary — DESIGN.md accent tokens, reused
       anywhere a session status needs a color (sidebar badge/dot/tooltip). */
    --status-running: #57c1ff;
    --status-permission: #ffc533;
    --status-permission-soft: rgba(255, 197, 51, 0.18);
    --status-idle: #434345;
    --status-error: #ff6161;
}
```

Token mapping notes (matches `DESIGN.md`'s color table):
- `--background` = canvas, `--card`/`--muted`/`--accent` = surface ladder
  steps (`surface` / `surface-card`), `--popover` = `surface-elevated` (used
  by the command palette's `bg-popover`).
- `--primary`/`--primary-foreground` = the white CTA pill + black text —
  `DESIGN.md`'s only primary action color.
- `--border`/`--input` = hairline (`#242728`), the universal 1px card edge.
- `--sidebar` = canvas too (sidebar sits directly on canvas per the
  confirmed design, no separate surface), `--sidebar-accent` = `surface-card`
  (active row background).
- `--radius: 0.625rem` (10px) already equals `DESIGN.md`'s `{rounded.lg}` —
  left unchanged, it was already correct.
- `--chart-*` are left untouched — no chart component is touched by this
  plan (out of scope).

- [ ] **Step 2: No automated check for CSS**

There's no CSS linter/typecheck wired into this project. This step is
verified visually in Task 9 (final manual QA) once the shell renders with
these tokens.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "style: apply DESIGN.md dark palette as the single theme root"
```

---

### Task 5: Trim dead shell CSS from app.css

**Files:**
- Modify: `frontend/src/app.css`

- [ ] **Step 1: Remove the `.workbench`/`.activity-*` block**

Delete these rules (lines 1-51 today) — they belong to `ActivityBar.tsx`,
which Task 8 deletes:

```css
.workbench {
  display: flex;
  min-height: 100vh;
  background: #101214;
  color: #f4f4f5;
}

.activity-bar {
  width: 120px;
  border-right: 1px solid #2a2f35;
  padding: 8px;
}

.activity-item {
  width: 100%;
  min-height: 40px;
  margin-bottom: 6px;
  background: transparent;
  color: inherit;
  border: 1px solid transparent;
  border-radius: 6px;
  cursor: pointer;
}

.activity-item.is-active {
  font-weight: 700;
  border-color: #3f4650;
  background: #181b20;
}

.workbench-main {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
}

.workbench-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 48px;
  border-bottom: 1px solid #2a2f35;
  padding: 0 16px;
}

.workbench-content {
  flex: 1;
  min-height: 0;
  padding: 16px;
}
```

The file should now start directly with `.tools-view { ... }`.

- [ ] **Step 2: Remove the `.command-palette*` block**

Delete these rules (between `.resolve-actions .resolve-merge-save` and
`.vault-gate`) — they belong to the old hand-rolled `CommandPalette.tsx`,
which Task 7 replaces with shadcn's `Command` primitives:

```css
.command-palette {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: start center;
  padding-top: 80px;
  background: rgba(0, 0, 0, 0.5);
}

.command-palette-panel {
  width: min(640px, calc(100vw - 32px));
  border: 1px solid #3f4650;
  border-radius: 8px;
  background: #181b20;
}

.command-palette-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #2a2f35;
  padding: 12px;
}

.command-palette-panel ul {
  margin: 0;
  padding: 12px 24px;
}
```

Everything else in `app.css` (`.notes-*`, `.database-view`, `.ssh-*`,
`.sync-*`, `.vault-gate`, `.agenthub-*`) stays untouched — those belong to
module content, out of scope for this pass.

- [ ] **Step 3: No automated check for CSS**

Same as Task 4 — verified visually in Task 9.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/app.css
git commit -m "chore: remove dead shell/command-palette CSS superseded by shadcn primitives"
```

---

### Task 6: AppSidebar

**Files:**
- Create: `frontend/src/workbench/AppSidebar.tsx`

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/workbench/AppSidebar.tsx
import { useState } from "react";
import {
  Bot,
  ChevronRight,
  Database,
  FileText,
  RefreshCw,
  Settings,
  Terminal,
  Workflow,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { useAgentSessionsStore } from "../stores/agentSessions";
import type { SessionStatus } from "../bridge/agenthub";

const MODULE_ICONS: Record<string, LucideIcon> = {
  tools: Wrench,
  notes: FileText,
  database: Database,
  ssh: Terminal,
  kafka: Workflow,
  agenthub: Bot,
  sync: RefreshCw,
  settings: Settings,
};

export type SidebarModule = { id: string; title: string; unavailable?: boolean };

type AppSidebarProps = {
  modules: SidebarModule[];
  activeModuleId: string;
  onSelectModule: (moduleId: string) => void;
};

function StatusDot({ status }: { status: SessionStatus }) {
  const colorClass =
    status === "running"
      ? "bg-(--status-running)"
      : status === "awaiting_permission"
        ? "bg-(--status-permission)"
        : status === "error"
          ? "bg-(--status-error)"
          : "bg-(--status-idle)";
  return <span className={cn("size-1.5 shrink-0 rounded-full", colorClass)} />;
}

function sessionLabel(session: { title: string | null; agentType: string; id: string }) {
  return session.title?.trim() || `${session.agentType} · ${session.id.slice(0, 8)}`;
}

export function AppSidebar({ modules, activeModuleId, onSelectModule }: AppSidebarProps) {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const [agentHubOpen, setAgentHubOpen] = useState(true);
  const hasPermissionRequest = sessions.some((s) => s.status === "awaiting_permission");

  return (
    <Sidebar variant="inset" collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <div className="flex size-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Bot className="size-3.5" />
          </div>
          <span className="text-sm font-semibold group-data-[collapsible=icon]:hidden">
            DevKit
          </span>
        </div>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Workspace</SidebarGroupLabel>
          <SidebarMenu>
            {modules.map((module) => {
              const Icon = MODULE_ICONS[module.id] ?? Wrench;

              if (module.id !== "agenthub") {
                return (
                  <SidebarMenuItem key={module.id}>
                    <SidebarMenuButton
                      isActive={module.id === activeModuleId}
                      disabled={module.unavailable}
                      aria-disabled={module.unavailable}
                      tooltip={module.title}
                      onClick={() => onSelectModule(module.id)}
                    >
                      <Icon />
                      <span>{module.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              }

              const badgeContent = sessions.length > 0 ? String(sessions.length) : null;

              return (
                <Collapsible
                  key={module.id}
                  open={agentHubOpen}
                  onOpenChange={setAgentHubOpen}
                  className="group/collapsible"
                >
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={module.id === activeModuleId}
                      disabled={module.unavailable}
                      aria-disabled={module.unavailable}
                      onClick={() => onSelectModule(module.id)}
                      tooltip={
                        sessions.length > 0
                          ? {
                              children: (
                                <div className="flex flex-col gap-1">
                                  {sessions.map((s) => (
                                    <div key={s.id} className="flex items-center gap-2 text-xs">
                                      <StatusDot status={s.status} />
                                      <span>{sessionLabel(s)}</span>
                                    </div>
                                  ))}
                                </div>
                              ),
                            }
                          : module.title
                      }
                    >
                      <Icon />
                      <span>{module.title}</span>
                    </SidebarMenuButton>

                    {badgeContent ? (
                      <SidebarMenuBadge
                        className={cn(
                          "group-data-[collapsible=icon]:right-0.5 group-data-[collapsible=icon]:top-0.5 group-data-[collapsible=icon]:flex group-data-[collapsible=icon]:size-2 group-data-[collapsible=icon]:min-w-0 group-data-[collapsible=icon]:p-0 group-data-[collapsible=icon]:text-[0px]",
                          hasPermissionRequest &&
                            "bg-(--status-permission-soft) text-(--status-permission)",
                        )}
                      >
                        {badgeContent}
                      </SidebarMenuBadge>
                    ) : null}

                    {sessions.length > 0 ? (
                      <CollapsibleTrigger asChild>
                        <button
                          type="button"
                          className="absolute top-1.5 right-7 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden"
                          aria-label={`Toggle ${module.title} sessions`}
                        >
                          <ChevronRight className="size-3.5 transition-transform group-data-[state=open]/collapsible:rotate-90" />
                        </button>
                      </CollapsibleTrigger>
                    ) : null}

                    <CollapsibleContent>
                      <SidebarMenuSub>
                        {sessions.map((s) => (
                          <SidebarMenuSubItem key={s.id}>
                            <SidebarMenuSubButton
                              onClick={() => {
                                onSelectModule("agenthub");
                                setActiveSessionId(s.id);
                              }}
                            >
                              <StatusDot status={s.status} />
                              <span>{sessionLabel(s)}</span>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        ))}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              );
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground group-data-[collapsible=icon]:justify-center">
          <span className="size-1.5 shrink-0 rounded-full bg-(--status-running)" />
          <span className="group-data-[collapsible=icon]:hidden">Vault unlocked</span>
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
```

Notes tying this back to the spec:
- `variant="inset"` on `Sidebar` is what makes `SidebarInset` (Task 8) render
  as the floating rounded card with a gap from the canvas frame — confirmed
  by reading `components/ui/sidebar.tsx` during design.
- The `group-data-[collapsible=icon]:...` classes on `SidebarMenuBadge`
  override that component's own baked-in
  `group-data-[collapsible=icon]:hidden` (via `cn()`/`twMerge`, which
  resolves same-variant conflicting utilities in favor of the later
  class) — turning the count badge into a 8px corner dot instead of
  disappearing when the sidebar collapses to icons.
- The `tooltip` prop on the Agent Hub `SidebarMenuButton` only renders when
  the sidebar is in `collapsed` state (built into
  `SidebarMenuButton` — see `hidden={state !== "collapsed"}` in
  `sidebar.tsx`), so hovering the collapsed icon shows the same session list
  the expanded sub-tree shows.
- `SidebarMenuBadge` and the manual `CollapsibleTrigger` chevron button are
  both `absolute`-positioned siblings of `SidebarMenuButton` inside the same
  `relative` `SidebarMenuItem` — this is the same layout shape
  `SidebarMenuAction` uses in the base component, not a new pattern.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/AppSidebar.tsx
git commit -m "feat(workbench): add DESIGN.md sidebar with Agent Hub status sub-tree"
```

---

### Task 7: Restyle Command Palette on shadcn Command primitives

**Files:**
- Modify: `frontend/src/workbench/CommandPalette.tsx` (full rewrite)

- [ ] **Step 1: Replace the file contents**

```tsx
// frontend/src/workbench/CommandPalette.tsx
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";

type CommandPaletteProps = {
  open: boolean;
  commands: Array<{ id: string; title: string }>;
  onClose: () => void;
};

export function CommandPalette({ open, commands, onClose }: CommandPaletteProps) {
  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
    >
      <CommandInput placeholder="Search commands..." />
      <CommandList>
        <CommandEmpty>No commands are currently available.</CommandEmpty>
        <CommandGroup heading="Commands">
          {commands.map((command) => (
            <CommandItem key={command.id} value={command.title} onSelect={onClose}>
              {command.title}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
```

`CommandDialog` already renders `bg-popover` (mapped to `surface-elevated`
in Task 4) with `rounded-xl` and no shadow beyond what Radix's `Dialog`
overlay provides — this is `DESIGN.md`'s `command-palette-card` component
essentially for free, no custom CSS needed. The previous version's manual
`open ? ... : null` guard is gone because `Dialog`/`CommandDialog` already
handle mount/unmount via Radix state internally.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/CommandPalette.tsx
git commit -m "feat(workbench): restyle command palette on shadcn Command primitives"
```

---

### Task 8: Rewire Workbench around AppSidebar + SidebarInset

**Files:**
- Create: `frontend/src/workbench/AgentSessionsSubscriber.tsx` (already created in Task 2)
- Delete: `frontend/src/workbench/ActivityBar.tsx`
- Modify: `frontend/src/workbench/Workbench.tsx` (full rewrite)

- [ ] **Step 1: Delete the old activity bar**

```bash
rm frontend/src/workbench/ActivityBar.tsx
```

- [ ] **Step 2: Replace Workbench.tsx**

```tsx
// frontend/src/workbench/Workbench.tsx
import { useEffect, useMemo, useState } from "react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { AppSidebar } from "./AppSidebar";
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { CommandPalette } from "./CommandPalette";
import { InteractionHost } from "./InteractionHost";
import {
  compareModuleParity,
  CURRENT_MODULE_API_VERSION,
  listRegisteredModules,
  registerModule,
} from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";
import { notesModule } from "../modules/notes/manifest";
import { databaseModule } from "../modules/database/manifest";
import { sshModule } from "../modules/ssh/manifest";
import { kafkaModule } from "../modules/kafka/manifest";
import { agenthubModule } from "../modules/agenthub/manifest";
import { syncModule } from "../modules/sync/manifest";
import { settingsModule } from "../modules/settings/manifest";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Kbd } from "@/components/ui/kbd";

registerModule(toolsModule);
registerModule(notesModule);
registerModule(databaseModule);
registerModule(sshModule);
registerModule(kafkaModule);
registerModule(agenthubModule);
registerModule(syncModule);
registerModule(settingsModule);

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [activeModuleId, setActiveModuleId] = useState(modules[0]?.id ?? "");
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    let mounted = true;
    listModules()
      .then((metadata) => {
        if (mounted) {
          setBackendModules(metadata);
        }
      })
      .catch(() => {
        if (mounted) {
          setMetadataError(true);
        }
      })
      .finally(() => {
        if (mounted) {
          setMetadataLoading(false);
        }
      });

    return () => {
      mounted = false;
    };
  }, []);

  const parityIssues = useMemo(
    () => (backendModules ? compareModuleParity(modules, backendModules) : []),
    [backendModules, modules],
  );
  const unavailableModuleIds = useMemo(() => {
    if (metadataError || !backendModules) {
      return new Set<string>();
    }

    const unavailable = new Set(
      parityIssues
        .map((issue) => issue.moduleId)
        .filter((moduleId) => moduleId !== "<unknown>"),
    );
    for (const metadata of backendModules) {
      if (metadata.apiVersion !== CURRENT_MODULE_API_VERSION && metadata.id) {
        unavailable.add(metadata.id);
      }
    }
    return unavailable;
  }, [backendModules, metadataError, parityIssues]);
  const availableModules = useMemo(
    () => modules.filter((module) => !unavailableModuleIds.has(module.id)),
    [modules, unavailableModuleIds],
  );
  const activityModules = useMemo(
    () =>
      modules.map((module) => ({
        id: module.id,
        title: module.title,
        unavailable: unavailableModuleIds.has(module.id),
      })),
    [modules, unavailableModuleIds],
  );
  const activeModule =
    availableModules.find((module) => module.id === activeModuleId) ??
    availableModules[0];
  const activeView = activeModule?.views[0];
  const ActiveView = activeView?.component;
  const commands = availableModules.flatMap((module) => module.commands);
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    <SidebarProvider>
      <AgentSessionsSubscriber />
      <AppSidebar
        activeModuleId={activeModule?.id ?? ""}
        modules={activityModules}
        onSelectModule={setActiveModuleId}
      />
      <SidebarInset className="border border-border bg-card shadow-none">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
          <SidebarTrigger />
          <strong className="text-sm font-semibold">
            {activeModule?.title ?? "DevKit"}
          </strong>
          {metadataLoading ? (
            <span role="status" className="text-xs text-muted-foreground">
              Loading module metadata...
            </span>
          ) : null}
          <div className="flex-1" />
          <Button variant="outline" size="sm" onClick={() => setPaletteOpen(true)}>
            Commands
            <Kbd>⌘K</Kbd>
          </Button>
        </header>
        {hasMetadataWarning ? (
          <Alert variant="destructive" className="m-4 mb-0 w-auto">
            <AlertDescription>
              Module metadata is unavailable or out of sync.
            </AlertDescription>
          </Alert>
        ) : null}
        <section className="flex-1 overflow-auto p-4">
          {ActiveView ? (
            <ActiveView />
          ) : (
            <p role="status" className="text-sm text-muted-foreground">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          )}
        </section>
      </SidebarInset>
      <CommandPalette
        open={paletteOpen}
        commands={commands}
        onClose={() => setPaletteOpen(false)}
      />
      <InteractionHost />
    </SidebarProvider>
  );
}
```

All the module-registration and parity-checking logic is untouched — only
the shell markup changed, from `<main className="workbench">` +
`ActivityBar` + plain header/content divs to `SidebarProvider` +
`AppSidebar` + `SidebarInset`. `SidebarInset`'s own default classes already
give the floating-card gap/rounding for `variant="inset"`; the added
`border border-border bg-card shadow-none` overrides its default
`bg-background shadow-sm` (via the same `cn()`/`twMerge` mechanism used in
Task 6) so the card renders on `--card` (surface) with a hairline border and
no drop shadow, per `DESIGN.md`.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors. If it complains about a missing `ActivityBar` import
somewhere else in the tree, grep for `from "./ActivityBar"` /
`from "../workbench/ActivityBar"` and remove that reference — nothing
outside `Workbench.tsx` should import it, but confirm.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git rm frontend/src/workbench/ActivityBar.tsx
git commit -m "feat(workbench): replace ActivityBar with DESIGN.md sidebar shell"
```

---

### Task 9: Wrap the app in TooltipProvider

**Files:**
- Modify: `frontend/src/App.tsx`

Radix's `Tooltip.Root` (used internally by `SidebarMenuButton`'s `tooltip`
prop, Task 6) expects a `Tooltip.Provider` ancestor. Nothing in the app
renders one today (`main.tsx` mounts `App` directly with no provider tree).

- [ ] **Step 1: Update App.tsx**

```tsx
// frontend/src/App.tsx
import "./app.css";
import { TooltipProvider } from "./components/ui/tooltip";
import { VaultGate } from "./workbench/VaultGate";
import { Workbench } from "./workbench/Workbench";

function App() {
  return (
    <TooltipProvider>
      <VaultGate>
        <Workbench />
      </VaultGate>
    </TooltipProvider>
  );
}

export default App;
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(workbench): wrap app in TooltipProvider for sidebar tooltips"
```

---

### Task 10: Manual verification

**Files:** none — this task runs the app, no code changes.

No frontend test runner exists, so this is the actual verification for
everything above. Use the `run` skill or start the Wails dev server directly
(check `Taskfile.yml` for the exact dev task, e.g. `task dev`).

- [ ] **Step 1: Start the dev server and open the app**

- [ ] **Step 2: Confirm the shell renders on DESIGN.md tokens**

Canvas frame visible behind the sidebar, content area is a separate rounded
card with a hairline border and a visible gap from the frame, no drop
shadows anywhere, all 8 modules listed with icons.

- [ ] **Step 3: Confirm cross-module Agent Hub status**

Open 2+ Agent Hub sessions (different vendors or same), send a prompt in
one, then switch to Database. Confirm the sidebar's Agent Hub row still
shows the session count badge, and expanding it shows each session with the
right colored status dot.

- [ ] **Step 4: Confirm collapsed-sidebar status**

Click the sidebar trigger to collapse it to icon-only. Confirm the Agent Hub
icon still shows a small corner dot (not the full badge) and hovering it
shows a tooltip listing the sessions with their status dots.

- [ ] **Step 5: Confirm the command palette**

Press ⌘K (or click "Commands" in the header). Confirm it opens styled
on-brand (dark popover, rounded, no light-mode flash), and typing filters
the list.

- [ ] **Step 6: Confirm module switching still works end-to-end**

Click through all 8 sidebar rows, confirm each renders its existing (unre-
styled) module content inside the new card without errors.

- [ ] **Step 7: Note results**

If anything fails, fix it and re-run the relevant earlier task's typecheck
step before re-verifying here. No commit for this task unless a fix was
needed, in which case commit that fix with a `fix(workbench): ...` message
describing what broke.

---

## Self-Review

**Spec coverage:** two-layer shell (Task 6+8, `variant="inset"`), fixed
labeled sidebar (Task 6), icon-heavy chrome (Task 6 `MODULE_ICONS`), status
color dots not icons (Task 6 `StatusDot`), Agent Hub accordion sub-tree
(Task 6), Agent Hub content view drops its tab-strip (Task 3), active row
styling via token remap (Task 4, `--sidebar-accent`), Command Palette
restyle (Task 7), Zustand cross-module store (Tasks 1-3), collapsed-mode
status via `tooltip` prop + badge override (Task 6), vault footer as static
badge (Task 6 `SidebarFooter`) — every decision in the spec has a task.

**Placeholder scan:** no TBD/TODO, every step has complete code.

**Type consistency:** `useAgentSessionsStore`'s `upsertSession`/
`setActiveSessionId`/`sessions`/`activeSessionId` are defined once in Task 1
and used with the same names in Tasks 2, 3, 6 — no drift. `SidebarModule`
type (Task 6) matches the shape `Workbench.tsx`'s existing `activityModules`
memo already produces (Task 8) — no adapter needed between them.
