import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  DockviewReact,
  type DockviewApi,
  type DockviewGroupPanel,
  type DockviewReadyEvent,
  type IDockviewHeaderActionsProps,
  type IDockviewPanel,
  type IDockviewPanelHeaderProps,
} from "dockview-react";
import "dockview-react/dist/styles/dockview.css";
import { Bot, Braces, Columns2, FileText, GitBranch, Maximize2, Minimize2, MoveRight, PanelBottom, PictureInPicture2, SquareTerminal, Workflow, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { PANEL_COMPONENTS } from "./panels";
import { canDrop, groupKindOf, relativeDirection, type ModuleId, type PaneParams, type Placement } from "@/workbench/dock/layoutRules";
import { forgetMounts, updateProbes, useProbes } from "./probes";
import { setDropHintElement, startLiftDrag } from "@/workbench/dock/liftDrag";
import "@/workbench/dock/dock.css";

/**
 * SPIKE — docs/dev-kit/layout.mdx on dockview. Reached at `?dock-spike`
 * (add `&pointer` to try the pointer-events DnD strategy meant for embedded
 * webviews). Not wired into the Workbench; throw-away once the spike is read.
 */

type CatalogEntry = { component: keyof typeof PANEL_COMPONENTS; params: PaneParams; keepAlive?: boolean };

const CATALOG: Record<string, CatalogEntry> = {
  "doc.codegraph.refunds": { component: "canvas", params: { kind: "document", module: "codegraph", title: "POST /refunds" }, keepAlive: true },
  "doc.codegraph.orders": { component: "canvas", params: { kind: "document", module: "codegraph", title: "GET /orders" }, keepAlive: true },
  "doc.notes.refund": { component: "editor", params: { kind: "document", module: "notes", title: "Refund notes" } },
  "doc.notes.ideas": { component: "editor", params: { kind: "document", module: "notes", title: "API design ideas" } },
  "ctx.codegraph.code": { component: "code", params: { kind: "context", module: "codegraph", title: "RefundService.java" } },
  "ctx.codegraph.dataflow": { component: "dataflow", params: { kind: "context", module: "codegraph", title: "Data flow · amount" } },
  "ctx.notes.metadata": { component: "metadata", params: { kind: "context", module: "notes", title: "Metadata · Refund notes" } },
  "cmp.agent": { component: "agent", params: { kind: "companion", title: "Agent · refund-worker" }, keepAlive: true },
  "cmp.terminal": { component: "terminal", params: { kind: "companion", title: "Terminal" }, keepAlive: true },
};

const MODULE_DOCS: Record<ModuleId, string[]> = {
  codegraph: ["doc.codegraph.refunds"],
  notes: ["doc.notes.refund"],
};
const MODULE_LABEL: Record<ModuleId, string> = { codegraph: "Code Graph", notes: "Notes" };

/** layout.mdx § Hai tầng layout: perModule, seeded with each module's preset 1. */
const perModule: Record<ModuleId, Placement[]> = {
  codegraph: [
    { panelId: "ctx.codegraph.code", reference: "document", direction: "right", size: 460 },
    { panelId: "ctx.codegraph.dataflow", reference: "ctx.codegraph.code", direction: "below", size: 280 },
  ],
  notes: [{ panelId: "ctx.notes.metadata", reference: "document", direction: "right", size: 300 }],
};

const paramsOf = (p: IDockviewPanel | undefined) => p?.params as PaneParams | undefined;
const groupParams = (g: DockviewGroupPanel) => groupKindOf(g.panels.map((p) => p.params as PaneParams));
const rectOf = (g: DockviewGroupPanel) => {
  const r = g.element.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
};

function addPanel(api: DockviewApi, id: string, position?: Parameters<DockviewApi["addPanel"]>[0]["position"], size?: { w?: number; h?: number }) {
  const e = CATALOG[id];
  return api.addPanel({
    id,
    component: e.component,
    tabComponent: "tab",
    title: e.params.title,
    params: e.params,
    renderer: e.keepAlive ? "always" : "onlyWhenVisible",
    position,
    initialWidth: size?.w,
    initialHeight: size?.h,
    minimumWidth: e.params.kind === "document" ? 360 : 240,
  });
}

function documentGroups(api: DockviewApi) {
  return api.groups.filter((g) => groupParams(g).kind === "document");
}
function visibleModules(api: DockviewApi): Set<ModuleId> {
  return new Set(documentGroups(api).map((g) => groupParams(g).module!).filter(Boolean));
}

/** Remember where a module's context panes are, relative to its document group. */
function capturePlacements(api: DockviewApi, module: ModuleId, docGroup: DockviewGroupPanel) {
  const panes = api.panels.filter((p) => paramsOf(p)?.kind === "context" && paramsOf(p)?.module === module);
  if (panes.length === 0) return;
  const seen: { id: string; group: DockviewGroupPanel }[] = [];
  perModule[module] = panes.map((p) => {
    const g = p.group;
    const same = seen.find((s) => s.group === g);
    let placement: Placement;
    if (same) placement = { panelId: p.id, reference: same.id, direction: "within" };
    else {
      const neighbour = seen.find((s) => relativeDirection(rectOf(g), rectOf(s.group)) !== "within");
      const ref = neighbour ? rectOf(neighbour.group) : rectOf(docGroup);
      const direction = relativeDirection(rectOf(g), ref);
      const r = rectOf(g);
      placement = {
        panelId: p.id,
        reference: neighbour?.id ?? "document",
        direction,
        size: direction === "below" || direction === "above" ? r.bottom - r.top : r.right - r.left,
      };
    }
    seen.push({ id: p.id, group: g });
    return placement;
  });
}

function restorePlacements(api: DockviewApi, module: ModuleId, docGroup: DockviewGroupPanel) {
  for (const pl of perModule[module]) {
    if (api.getPanel(pl.panelId)) continue;
    const vertical = pl.direction === "below" || pl.direction === "above";
    const size = vertical ? { h: pl.size } : { w: pl.size };
    if (pl.reference === "document") addPanel(api, pl.panelId, { referenceGroup: docGroup, direction: pl.direction }, size);
    else if (api.getPanel(pl.reference)) addPanel(api, pl.panelId, { referencePanel: pl.reference, direction: pl.direction }, size);
    else addPanel(api, pl.panelId, { referenceGroup: docGroup, direction: "right" }, size);
  }
}

function removeContextPanes(api: DockviewApi, module: ModuleId) {
  for (const p of [...api.panels]) if (paramsOf(p)?.kind === "context" && paramsOf(p)?.module === module) api.removePanel(p);
}

// ── Presets (layout.mdx § Preset) ───────────────────────────────────────────
const companionRect = (api: DockviewApi, id: string) => {
  const p = api.getPanel(id);
  return p ? rectOf(p.group) : null;
};

function buildReviewCode(api: DockviewApi) {
  api.clear();
  const doc = addPanel(api, "doc.codegraph.refunds");
  restorePlacements(api, "codegraph", doc.group);
  const term = addPanel(api, "cmp.terminal", undefined);
  api.addFloatingGroup(term, { x: 300, y: 420, width: 540, height: 240 });
}

function buildPairWithAgent(api: DockviewApi) {
  api.clear();
  const doc = addPanel(api, "doc.codegraph.refunds");
  restorePlacements(api, "codegraph", doc.group);
  addPanel(api, "cmp.agent", { direction: "right" }, { w: 340 });
}

function buildFocus(api: DockviewApi) {
  const docs = api.panels.filter((p) => paramsOf(p)?.kind === "document").map((p) => p.id);
  for (const m of visibleModules(api)) {
    const g = documentGroups(api).find((x) => groupParams(x).module === m);
    if (g) capturePlacements(api, m, g);
  }
  for (const p of [...api.panels]) if (!docs.includes(p.id)) api.removePanel(p);
}

// ── Tab + header actions ────────────────────────────────────────────────────
function iconFor(p?: PaneParams) {
  if (!p) return FileText;
  if (p.kind === "companion") return p.title.startsWith("Terminal") ? SquareTerminal : Bot;
  if (p.module === "codegraph") return p.kind === "document" ? Workflow : p.title.startsWith("Data") ? GitBranch : Braces;
  return FileText;
}

// Lift drag unless `&native` asks for dockview's own drag (chip / HTML5).
const LIFT = !new URLSearchParams(window.location.search).has("native");

function Tab({ api, containerApi, params }: IDockviewPanelHeaderProps<PaneParams>) {
  const Icon = iconFor(params);
  const ref = useRef<HTMLDivElement>(null);
  // A native listener on the tab itself: dockview handles pointerdown on its
  // tab element and it never reaches React's root-delegated handler.
  useEffect(() => {
    const el = ref.current;
    if (!LIFT || !el) return;
    const onDown = (e: PointerEvent) => {
      if ((e.target as HTMLElement).closest("button")) return;
      const panel = containerApi.getPanel(api.id);
      if (panel) startLiftDrag(containerApi, panel, e);
    };
    el.addEventListener("pointerdown", onDown);
    return () => el.removeEventListener("pointerdown", onDown);
  }, [api.id, containerApi]);
  return (
    <div ref={ref} className="flex h-full items-center gap-1.5 px-2 text-body-13">
      <Icon className="size-3.5 shrink-0" />
      <span className="truncate">{api.title}</span>
      <button type="button" aria-label={`Close ${api.title}`} className="ml-1 flex size-4 items-center justify-center rounded-sm opacity-60 hover:bg-state-soft hover:opacity-100" onClick={(e) => { e.stopPropagation(); api.close(); }}>
        <X className="size-3" />
      </button>
    </div>
  );
}

const actionBtn = "flex size-6 items-center justify-center rounded-md text-text-tertiary hover:bg-state-soft hover:text-text-primary";

function HeaderActions({ group, containerApi, activePanel }: IDockviewHeaderActionsProps) {
  const kind = groupParams(group).kind;
  const [menu, setMenu] = useState(false);
  const floating = group.api.location.type === "floating";
  const maximized = activePanel?.api.isMaximized() ?? false;
  if (!activePanel) return null;
  const move = (position: "right" | "bottom" | "left") => {
    const ref = documentGroups(containerApi)[0];
    activePanel.api.moveTo({ group: ref ?? group, position });
    setMenu(false);
  };
  return (
    <div className="relative flex h-full items-center gap-0.5 pr-1">
      {kind === "document" ? (
        <button type="button" aria-label="Split right (⌘\\)" title="Split right (⌘\)" className={actionBtn} onClick={() => activePanel.api.moveTo({ group, position: "right" })}>
          <Columns2 className="size-3.5" />
        </button>
      ) : (
        <>
          <button type="button" aria-label="Move to…" aria-expanded={menu} title="Move to…" className={actionBtn} onClick={() => setMenu((m) => !m)}>
            <MoveRight className="size-3.5" />
          </button>
          <button
            type="button"
            aria-label={floating ? "Dock" : "Float"}
            title={floating ? "Dock" : "Float"}
            className={actionBtn}
            onClick={() => (floating ? activePanel.api.moveTo({ group: documentGroups(containerApi)[0], position: "right" }) : containerApi.addFloatingGroup(activePanel, { width: 480, height: 280 }))}
          >
            <PictureInPicture2 className="size-3.5" />
          </button>
        </>
      )}
      {!floating && (
        <button type="button" aria-label={maximized ? "Restore" : "Maximize"} title="Maximize (⌘⇧↩)" className={actionBtn} onClick={() => (maximized ? activePanel.api.exitMaximized() : activePanel.api.maximize())}>
          {maximized ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
        </button>
      )}
      {menu && (
        <div role="menu" className="absolute top-8 right-0 z-50 w-44 rounded-lg border border-glass-border bg-surface-raised p-1 text-body-13 shadow-lg">
          {([["right", "Right of the document"], ["bottom", "Below the document"], ["left", "Left of the document"]] as const).map(([pos, label]) => (
            <button key={pos} role="menuitem" type="button" className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-text-primary hover:bg-state-soft" onClick={() => move(pos)}>
              {pos === "bottom" ? <PanelBottom className="size-3.5" /> : <MoveRight className="size-3.5" />}
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ── Probe card ──────────────────────────────────────────────────────────────
function ProbeCard() {
  const p = useProbes();
  const remounted = Object.entries(p.mounts).filter(([, n]) => n > 1);
  const zeroCanvas = Object.entries(p.canvasSize).filter(([, s]) => s.w === 0 || s.h === 0);
  const row = (ok: boolean | null, label: string, detail: string) => (
    <li className="flex gap-2">
      <span className={cn("w-4 shrink-0", ok === null ? "text-text-tertiary" : ok ? "text-status-success" : "text-status-danger")}>{ok === null ? "·" : ok ? "✓" : "✗"}</span>
      <span className="text-text-primary">{label}</span>
      <span className="ml-auto truncate text-text-tertiary">{detail}</span>
    </li>
  );
  return (
    <ul className="space-y-1 text-caption-11">
      {row(remounted.length === 0, "No panel re-mounted by a move", remounted.map(([id, n]) => `${id}×${n}`).join(", ") || "—")}
      {row(p.terminalsCreated <= 1, "One xterm, ever", `created ${p.terminalsCreated}`)}
      {row(Object.keys(p.canvasSize).length ? zeroCanvas.length === 0 : null, "Canvas never stuck at 0", Object.entries(p.canvasSize).map(([id, s]) => `${id.split(".").pop()} ${s.w}×${s.h}`).join(" "))}
      {row(p.companionMovedOnSwitch === null ? null : !p.companionMovedOnSwitch, "Companions still on module switch", p.lastSwitchMs === null ? "switch a module" : `${p.lastSwitchMs.toFixed(1)} ms`)}
      {row(p.blockedDrops > 0 ? true : null, "Illegal drops refused", `${p.blockedDrops}`)}
      {row(p.lastRestore === null ? null : true, "Corrupt layout recovers", p.lastRestore ?? "—")}
    </ul>
  );
}

// ── The spike page ──────────────────────────────────────────────────────────
export default function DockSpike() {
  const apiRef = useRef<DockviewApi | null>(null);
  const [activeModule, setActiveModule] = useState<ModuleId>("codegraph");
  const pointer = new URLSearchParams(window.location.search).has("pointer");

  const syncGroupKinds = useCallback((api: DockviewApi) => {
    for (const g of api.groups) {
      const k = groupParams(g);
      if (k.kind) g.element.dataset.dkKind = k.kind;
      else delete g.element.dataset.dkKind;
    }
  }, []);

  const onReady = useCallback(
    (e: DockviewReadyEvent) => {
      const api = e.api;
      apiRef.current = api;
      // Spike only: lets the checklist be driven from devtools / automation.
      (window as unknown as { __dockSpike?: DockviewApi }).__dockSpike = api;
      buildReviewCode(api);
      syncGroupKinds(api);
      api.onDidLayoutChange(() => syncGroupKinds(api));
      api.onDidRemovePanel((p) => forgetMounts(p.id));
      // layout.mdx § Document group: the focused *document* group decides the
      // active module; focusing a pane leaves it alone.
      api.onDidActiveGroupChange((g) => {
        const k = g ? groupParams(g) : undefined;
        if (k?.kind === "document" && k.module) setActiveModule(k.module);
      });
      api.onWillShowOverlay((ev) => {
        const data = ev.getData();
        const dragged = data?.panelId ? paramsOf(api.getPanel(data.panelId)) : data ? paramsOf(api.getGroup(data.groupId)?.activePanel) : undefined;
        if (!dragged) return;
        const t = ev.group ? groupParams(ev.group) : {};
        if (!canDrop(dragged, { groupKind: t.kind, groupModule: t.module, location: ev.kind, position: ev.position })) {
          ev.preventDefault();
          updateProbes((s) => ({ blockedDrops: s.blockedDrops + 1 }));
        }
      });
    },
    [syncGroupKinds],
  );

  /** Switch the focused document group to another module (layout.mdx § Hai tầng). */
  const switchModule = useCallback((to: ModuleId) => {
    const api = apiRef.current;
    if (!api) return;
    const group = (api.activeGroup && groupParams(api.activeGroup).kind === "document" ? api.activeGroup : documentGroups(api)[0]) as DockviewGroupPanel | undefined;
    if (!group) return;
    const from = groupParams(group).module!;
    if (from === to) return;
    const before = { agent: companionRect(api, "cmp.agent"), terminal: companionRect(api, "cmp.terminal") };
    // dockview redistributes space proportionally when a column is added or
    // removed, so a companion would shrink a little on every switch. Pin their
    // sizes and let the document absorb the difference (layout.mdx: the
    // companion layer does not move on a module switch).
    const companionSizes = (["cmp.agent", "cmp.terminal"] as const).flatMap((id) => {
      const p = api.getPanel(id);
      if (!p || p.group.api.location.type !== "grid") return [];
      // The grid's own size, not the element's: the theme's gutter padding
      // lives between them, and mixing the two drifts 8px per switch.
      return [{ group: p.group, width: p.group.api.width, height: p.group.api.height }];
    });
    const t0 = performance.now();
    const fromStillVisible = documentGroups(api).some((g) => g !== group && groupParams(g).module === from);
    if (!fromStillVisible) capturePlacements(api, from, group);
    const oldDocs = [...group.panels];
    for (const id of MODULE_DOCS[to]) {
      if (api.getPanel(id)) api.getPanel(id)!.api.moveTo({ group, position: "center" });
      else addPanel(api, id, { referenceGroup: group, direction: "within" });
    }
    for (const p of oldDocs) api.removePanel(p);
    if (!fromStillVisible) removeContextPanes(api, from);
    restorePlacements(api, to, group);
    for (const c of companionSizes) c.group.api.setSize({ width: c.width, height: c.height });
    const syncMs = performance.now() - t0;
    // setTimeout, not rAF: a hidden tab never runs rAF, and the probe must report.
    setTimeout(() => {
      const after = { agent: companionRect(api, "cmp.agent"), terminal: companionRect(api, "cmp.terminal") };
      const moved = (a: ReturnType<typeof companionRect>, b: ReturnType<typeof companionRect>) =>
        !!a && !!b && (Math.abs(a.left - b.left) > 1 || Math.abs(a.right - b.right) > 1 || Math.abs(a.top - b.top) > 1);
      updateProbes(() => ({ lastSwitchMs: syncMs, companionMovedOnSwitch: moved(before.agent, after.agent) || moved(before.terminal, after.terminal) }));
    }, 50);
    setActiveModule(to);
  }, []);

  const openBeside = useCallback((module: ModuleId) => {
    const api = apiRef.current;
    if (!api) return;
    const groups = documentGroups(api);
    const ref = groups[groups.length - 1];
    const id = MODULE_DOCS[module].find((d) => !api.getPanel(d)) ?? (module === "notes" ? "doc.notes.ideas" : "doc.codegraph.orders");
    if (api.getPanel(id)) return;
    const p = addPanel(api, id, ref ? { referenceGroup: ref, direction: "right" } : undefined);
    if (!visibleModules(api).has(module) || !api.panels.some((x) => paramsOf(x)?.kind === "context" && paramsOf(x)?.module === module)) restorePlacements(api, module, p.group);
  }, []);

  const toggleCompanion = useCallback((id: "cmp.agent" | "cmp.terminal") => {
    const api = apiRef.current;
    if (!api) return;
    const p = api.getPanel(id);
    if (p) api.removePanel(p);
    else if (id === "cmp.agent") addPanel(api, id, { direction: "right" }, { w: 340 });
    else addPanel(api, id, { direction: "below" }, { h: 220 });
  }, []);

  const saveLayout = () => localStorage.setItem("dockSpike.layout", JSON.stringify(apiRef.current?.toJSON()));
  const restoreLayout = (corrupt = false) => {
    const api = apiRef.current;
    if (!api) return;
    try {
      const raw = corrupt ? { grid: { root: "not a tree" }, panels: 42 } : JSON.parse(localStorage.getItem("dockSpike.layout") ?? "null");
      api.fromJSON(raw, { reuseExistingPanels: true });
      updateProbes(() => ({ lastRestore: "ok" }));
    } catch {
      // layout.mdx § Lưu trữ: an unreadable layout falls back to preset 1.
      buildReviewCode(api);
      updateProbes(() => ({ lastRestore: "recovered" }));
    }
    syncGroupKinds(api);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const api = apiRef.current;
      if (!api) return;
      if (e.metaKey && e.key === "\\") {
        const p = api.activePanel;
        if (p && paramsOf(p)?.kind === "document") p.api.moveTo({ group: p.group, position: "right" });
        e.preventDefault();
      } else if (e.ctrlKey && e.key === "1") { buildFocus(api); e.preventDefault(); }
      else if (e.ctrlKey && e.key === "2") { buildReviewCode(api); e.preventDefault(); }
      else if (e.ctrlKey && e.key === "3") { buildPairWithAgent(api); e.preventDefault(); }
      else if (e.metaKey && e.key.toLowerCase() === "l") { toggleCompanion("cmp.agent"); e.preventDefault(); }
      else if (e.ctrlKey && e.key === "`") { toggleCompanion("cmp.terminal"); e.preventDefault(); }
      else if (e.metaKey && e.shiftKey && e.key === "Enter") {
        const p = api.activePanel;
        if (p) (p.api.isMaximized() ? p.api.exitMaximized() : p.api.maximize());
        e.preventDefault();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggleCompanion]);

  const hintPortal = createPortal(<div ref={setDropHintElement} className="dk-drop-hint" aria-hidden />, document.body);
  const btn = "rounded-md border border-border-subtle bg-surface-raised px-2 py-1 text-caption-11-medium text-text-primary hover:border-status-info";
  return (
    <div className="dark flex h-screen w-screen flex-col bg-bg-app text-text-primary">
      {hintPortal}
      <header className="flex h-10 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
        <span className="text-title-13">Dockview spike</span>
        <span className="text-caption-11 text-text-tertiary">{LIFT ? "drag: lift (live pane)" : pointer ? "drag: dockview pointer" : "drag: dockview html5"}</span>
        <button
          type="button"
          className={btn}
          onClick={() => {
            const url = new URL(window.location.href);
            if (LIFT) url.searchParams.set("native", "");
            else url.searchParams.delete("native");
            window.location.href = url.toString();
          }}
        >
          {LIFT ? "Use dockview drag" : "Use lift drag"}
        </button>
        <button
          type="button"
          className={btn}
          onClick={() => {
            const url = new URL(window.location.href);
            if (pointer) url.searchParams.delete("pointer");
            else url.searchParams.set("pointer", "");
            window.location.href = url.toString();
          }}
        >
          Switch to {pointer ? "HTML5" : "pointer"} DnD
        </button>
        <button type="button" className={btn} onClick={() => (window.location.href = window.location.pathname)}>
          Back to app (⌘⌥⇧D)
        </button>
        <button
          type="button"
          className={cn(btn, "border-status-info")}
          onClick={() => {
            // The shell trial reads this flag at startup (workbench/dock/dockShellFlag.ts).
            try {
              if (localStorage.getItem("devkit.dockShell") === "1") localStorage.removeItem("devkit.dockShell");
              else localStorage.setItem("devkit.dockShell", "1");
            } catch {
              return;
            }
            window.location.href = window.location.pathname;
          }}
        >
          {(() => {
            try {
              return localStorage.getItem("devkit.dockShell") === "1" ? "Dock shell: ON → turn off" : "Dock shell: OFF → turn on & open app";
            } catch {
              return "Dock shell";
            }
          })()}
        </button>
        <div className="ml-4 flex items-center gap-1">
          {(["codegraph", "notes"] as const).map((m) => (
            <button key={m} type="button" className={cn(btn, activeModule === m && "border-status-info")} onClick={() => switchModule(m)}>{MODULE_LABEL[m]}</button>
          ))}
          <button type="button" className={btn} onClick={() => openBeside("notes")}>Open note beside</button>
          <button type="button" className={btn} onClick={() => openBeside("codegraph")}>Open 2nd flow beside</button>
        </div>
        <div className="ml-auto flex items-center gap-1">
          <button type="button" className={btn} onClick={() => apiRef.current && buildFocus(apiRef.current)}>⌃1 Focus</button>
          <button type="button" className={btn} onClick={() => apiRef.current && buildReviewCode(apiRef.current)}>⌃2 Review code</button>
          <button type="button" className={btn} onClick={() => apiRef.current && buildPairWithAgent(apiRef.current)}>⌃3 Pair with agent</button>
          <button type="button" className={btn} onClick={() => toggleCompanion("cmp.agent")}>⌘L Agent</button>
          <button type="button" className={btn} onClick={() => toggleCompanion("cmp.terminal")}>⌃` Terminal</button>
          <button type="button" className={btn} onClick={saveLayout}>Save</button>
          <button type="button" className={btn} onClick={() => restoreLayout()}>Restore</button>
          <button type="button" className={btn} onClick={() => restoreLayout(true)}>Load corrupt</button>
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <aside className="flex w-[264px] shrink-0 flex-col gap-3 p-4" aria-label="Sidebar">
          <div className="text-caption-11 text-text-tertiary">SIDEBAR · follows the focused document group</div>
          <div className="text-title-14">{MODULE_LABEL[activeModule]}</div>
          <div className="text-body-12 text-text-secondary">
            {activeModule === "codegraph" ? "POST /refunds · GET /refunds/{id} · POST /orders" : "Refund notes · API design ideas · Q3 Planning"}
          </div>
          <div className="mt-auto rounded-lg border border-glass-border bg-surface-base p-3">
            <div className="mb-2 text-caption-11-medium text-text-secondary">Spike checklist (live)</div>
            <ProbeCard />
          </div>
        </aside>
        <div className="relative min-w-0 flex-1 p-1">
          <DockviewReact
            className="dockview-theme-devkit h-full"
            components={PANEL_COMPONENTS}
            tabComponents={{ tab: Tab }}
            rightHeaderActionsComponent={HeaderActions}
            onReady={onReady}
            floatingGroupBounds="boundedWithinViewport"
            dndStrategy={pointer ? "pointer" : "auto"}
            disableDnd={LIFT}
            disableTabsOverflowList
          />
        </div>
      </div>
    </div>
  );
}
