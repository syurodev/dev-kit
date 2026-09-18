import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import {
  DockviewReact,
  type DockviewApi,
  type DockviewGroupPanel,
  type DockviewReadyEvent,
  type DockviewTheme,
  type IDockviewHeaderActionsProps,
  type IDockviewPanelHeaderProps,
} from "dockview-react";
import "dockview-react/dist/styles/dockview.css";
import { Bot, Maximize2, Minimize2, MoveRight, PanelBottom, PanelRight, PictureInPicture2, SquareTerminal, X } from "lucide-react";
import { useSplitPanel } from "@/components/ui/split-panel";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { useBottomContentStore } from "@/stores/bottomContent";
import { TerminalWorkspace } from "@/workbench/TerminalWorkspace";
import { getModule } from "@/modules/registry";
import { canDrop, groupKindOf, relativeDirection, type PaneParams, type Placement } from "./layoutRules";
import { setDropHintElement, startLiftDrag } from "./liftDrag";
import "./dock.css";

/**
 * EXPERIMENT — the right and bottom of the shell as dockable panes
 * (docs/dev-kit/layout.mdx), behind devkit.dockShell. Lazy: dockview never
 * reaches the initial bundle while the flag is off.
 *
 * Nothing a module calls changes. openPanel, the agent companion and the
 * bottom slot keep their state where it is today; this only decides where
 * that state is drawn:
 *
 *   document  — the content card (tab strip + views), one "document host"
 *               pane, never closed, never floated, header hidden (option b)
 *   panel     — whatever the active module passed to openPanel (context pane,
 *               position remembered per module)
 *   agent     — the companion slot (companion pane, position global)
 *   terminal  — the bottom slot's TerminalWorkspace (companion pane)
 */

const DOC = "document";
const PANEL = "panel";
const AGENT = "agent";
const TERMINAL = "terminal";

type ShellCtx = {
  children: ReactNode;
  activeModuleId: string | null;
  close: (id: string) => void;
};
const Ctx = createContext<ShellCtx | null>(null);
const useShell = () => useContext(Ctx)!;

// ── Pane bodies ─────────────────────────────────────────────────────────────
function DocumentPane() {
  return <div className="flex h-full min-h-0 flex-col overflow-hidden">{useShell().children}</div>;
}
function ModulePanelPane() {
  const { panel } = useSplitPanel();
  return <div className="flex h-full min-h-0 flex-col overflow-auto">{panel?.content}</div>;
}
function AgentPane() {
  const { companion } = useSplitPanel();
  return <div className="flex h-full min-h-0 flex-col overflow-hidden">{companion?.content}</div>;
}
function TerminalPane() {
  const { activeModuleId } = useShell();
  const content = useBottomContentStore((s) => (activeModuleId ? s.content[activeModuleId] : undefined));
  return (
    <div className="relative flex h-full min-h-0 flex-col overflow-hidden">
      <TerminalWorkspace moduleContent={content ?? undefined} />
    </div>
  );
}
const COMPONENTS = { [DOC]: DocumentPane, [PANEL]: ModulePanelPane, [AGENT]: AgentPane, [TERMINAL]: TerminalPane };

// ── Tab and header ──────────────────────────────────────────────────────────
function iconFor(id: string) {
  return id === AGENT ? Bot : id === TERMINAL ? SquareTerminal : PanelRight;
}

function Tab({ api, containerApi }: IDockviewPanelHeaderProps<PaneParams>) {
  const { close } = useShell();
  const ref = useRef<HTMLDivElement>(null);
  const Icon = iconFor(api.id);
  // Native listener: dockview handles pointerdown on its tab element first.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onDown = (e: PointerEvent) => {
      if ((e.target as HTMLElement).closest("button")) return;
      // Before the 5px threshold the pointer would otherwise start a text
      // selection across whatever it crosses (Safari keeps it after the drag).
      e.preventDefault();
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
      <button
        type="button"
        aria-label={`Close ${api.title}`}
        className="ml-1 flex size-4 items-center justify-center rounded-sm opacity-60 hover:bg-state-soft hover:opacity-100"
        onClick={(e) => {
          e.stopPropagation();
          close(api.id);
        }}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

const actionBtn = "flex size-6 items-center justify-center rounded-md text-text-tertiary hover:bg-state-soft hover:text-text-primary";

function HeaderActions({ group, containerApi, activePanel }: IDockviewHeaderActionsProps) {
  const [menu, setMenu] = useState(false);
  if (!activePanel || activePanel.id === DOC) return null;
  const floating = group.api.location.type === "floating";
  const maximized = activePanel.api.isMaximized();
  const doc = containerApi.getPanel(DOC)?.group;
  const move = (position: "right" | "bottom" | "left") => {
    if (doc) activePanel.api.moveTo({ group: doc, position });
    setMenu(false);
  };
  return (
    <div className="relative flex h-full items-center gap-0.5 pr-1">
      <button type="button" aria-label="Move to…" aria-expanded={menu} title="Move to…" className={actionBtn} onClick={() => setMenu((m) => !m)}>
        <MoveRight className="size-3.5" />
      </button>
      <button
        type="button"
        aria-label={floating ? "Dock" : "Float"}
        title={floating ? "Dock" : "Float"}
        className={actionBtn}
        onClick={() => (floating ? doc && activePanel.api.moveTo({ group: doc, position: "right" }) : containerApi.addFloatingGroup(activePanel, { width: 480, height: 300 }))}
      >
        <PictureInPicture2 className="size-3.5" />
      </button>
      {!floating && (
        <button type="button" aria-label={maximized ? "Restore" : "Maximize"} className={actionBtn} onClick={() => (maximized ? activePanel.api.exitMaximized() : activePanel.api.maximize())}>
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

// ── Placement memory (layout.mdx § Vị trí nhớ) ──────────────────────────────
const placements = new Map<string, Placement>();
const groupRect = (g: DockviewGroupPanel) => {
  const r = g.element.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
};

function remember(api: DockviewApi, id: string, key: string) {
  const p = api.getPanel(id);
  const doc = api.getPanel(DOC)?.group;
  if (!p || !doc || p.group.api.location.type !== "grid") return;
  const direction = relativeDirection(groupRect(p.group), groupRect(doc));
  if (direction === "within") return; // sharing the document's group is not allowed anyway
  const vertical = direction === "below" || direction === "above";
  placements.set(key, { panelId: id, reference: "document", direction, size: vertical ? p.group.api.height : p.group.api.width });
}

/**
 * Add or remove a pane, keeping the companions' sizes: dockview shares space
 * out proportionally when a column comes or goes, so without this a
 * companion shrinks a little every time the module panel does.
 */
function withCompanionsPinned(api: DockviewApi, fn: () => void) {
  const pinned = [AGENT, TERMINAL].flatMap((id) => {
    const p = api.getPanel(id);
    return p && p.group.api.location.type === "grid" ? [{ g: p.group, width: p.group.api.width, height: p.group.api.height }] : [];
  });
  fn();
  for (const c of pinned) c.g.api.setSize({ width: c.width, height: c.height });
}

function ensurePane(
  api: DockviewApi,
  id: string,
  want: boolean,
  opts: { key: string; title: string; params: PaneParams; fallback: Placement },
) {
  const existing = api.getPanel(id);
  if (!want) {
    if (existing) {
      remember(api, id, opts.key);
      withCompanionsPinned(api, () => api.removePanel(existing));
    }
    return;
  }
  if (existing) {
    existing.api.setTitle(opts.title);
    existing.api.updateParameters(opts.params);
    return;
  }
  const doc = api.getPanel(DOC)?.group;
  const pl = placements.get(opts.key) ?? opts.fallback;
  const vertical = pl.direction === "below" || pl.direction === "above";
  withCompanionsPinned(api, () => {
    api.addPanel({
      id,
      component: id,
      tabComponent: "tab",
      title: opts.title,
      params: opts.params,
      renderer: "always",
      minimumWidth: 280,
      minimumHeight: 120,
      initialWidth: vertical ? undefined : pl.size,
      initialHeight: vertical ? pl.size : undefined,
      position: pl.reference === "edge" || !doc ? { direction: pl.direction === "within" ? "right" : pl.direction } : { referenceGroup: doc, direction: pl.direction },
    });
  });
}

// ── The row ─────────────────────────────────────────────────────────────────
export function DockShellRow({ activeModuleId, children }: { activeModuleId: string | null; children: ReactNode }) {
  const [api, setApi] = useState<DockviewApi | null>(null);
  const { panel, companion } = useSplitPanel();
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const bottomHeight = useBottomSlotStore((s) => s.height);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);

  const close = useCallback(
    (id: string) => {
      if (id === PANEL) panel?.onClose();
      else if (id === AGENT) companion?.onClose();
      else if (id === TERMINAL) setBottomOpen(false);
    },
    [panel, companion, setBottomOpen],
  );

  const onReady = useCallback((e: DockviewReadyEvent) => {
    const a = e.api;
    const doc = a.addPanel({
      id: DOC,
      component: DOC,
      title: "Document",
      params: { kind: "document", title: "Document" } satisfies PaneParams,
      renderer: "always",
      minimumWidth: 480,
    });
    doc.group.model.header.hidden = true;
    a.onWillShowOverlay((ev) => {
      const data = ev.getData();
      const dragged = (data?.panelId ? a.getPanel(data.panelId)?.params : undefined) as PaneParams | undefined;
      if (!dragged) return;
      const t = ev.group ? groupKindOf(ev.group.panels.map((p) => p.params as PaneParams)) : {};
      if (!canDrop(dragged, { groupKind: t.kind, groupModule: t.module, location: ev.kind, position: ev.position })) ev.preventDefault();
    });
    // The document's group must never show a header, whatever moves around it.
    a.onDidLayoutChange(() => {
      const g = a.getPanel(DOC)?.group;
      if (g && !g.model.header.hidden) g.model.header.hidden = true;
      for (const grp of a.groups) {
        const k = groupKindOf(grp.panels.map((p) => p.params as PaneParams)).kind;
        if (k) grp.element.dataset.dkKind = k;
      }
    });
    setApi(a);
  }, []);

  // Panel: per module. Switching module with a panel open swaps whose
  // placement is used, so each module's panel comes back where it was left.
  const lastPanelModule = useRef<string | null>(null);
  useEffect(() => {
    if (!api) return;
    const moduleId = panel ? activeModuleId : null;
    if (lastPanelModule.current && lastPanelModule.current !== moduleId && api.getPanel(PANEL)) {
      ensurePane(api, PANEL, false, { key: `panel:${lastPanelModule.current}`, title: "", params: { kind: "context", title: "" }, fallback: FALLBACK_PANEL });
    }
    const title = moduleId ? (getModule(moduleId)?.title ?? "Panel") : "Panel";
    ensurePane(api, PANEL, moduleId !== null, {
      key: `panel:${moduleId ?? lastPanelModule.current}`,
      title,
      params: { kind: "context", module: moduleId ?? undefined, title },
      fallback: { ...FALLBACK_PANEL, size: panel?.width ?? FALLBACK_PANEL.size },
    });
    lastPanelModule.current = moduleId;
  }, [api, panel, activeModuleId]);

  useEffect(() => {
    if (!api) return;
    ensurePane(api, AGENT, companion !== null, { key: AGENT, title: "Agent", params: { kind: "companion", title: "Agent" }, fallback: FALLBACK_AGENT });
  }, [api, companion]);

  useEffect(() => {
    if (!api) return;
    ensurePane(api, TERMINAL, bottomOpen, {
      key: TERMINAL,
      title: "Terminal",
      params: { kind: "companion", title: "Terminal" },
      fallback: { ...FALLBACK_TERMINAL, size: bottomHeight },
    });
    // Height is read once, at open, like the bottom slot's own default.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, bottomOpen]);

  return (
    <Ctx.Provider value={{ children, activeModuleId, close }}>
      {createPortal(<div ref={setDropHintElement} className="dk-drop-hint" aria-hidden />, document.body)}
      {/* dockview 8 wraps its root in a .dv-shell (edge groups) that carries
          the theme class, so the theme goes through `theme`, not className —
          otherwise the shell keeps dockview's default abyss theme. The host
          div is what the row sizes and what takes the negative margin. */}
      <div className="dk-shell-host">
      <DockviewReact
        theme={DEVKIT_THEME}
        className="dk-shell h-full w-full"
        components={COMPONENTS}
        tabComponents={{ tab: Tab }}
        rightHeaderActionsComponent={HeaderActions}
        onReady={onReady}
        floatingGroupBounds="boundedWithinViewport"
        disableDnd
        disableTabsOverflowList
      />
      </div>
    </Ctx.Provider>
  );
}

const DEVKIT_THEME: DockviewTheme = { name: "devkit", className: "dockview-theme-devkit" };

const FALLBACK_PANEL: Placement = { panelId: PANEL, reference: "document", direction: "right", size: 420 };
const FALLBACK_AGENT: Placement = { panelId: AGENT, reference: "edge", direction: "right", size: 360 };
const FALLBACK_TERMINAL: Placement = { panelId: TERMINAL, reference: "document", direction: "below", size: 220 };

export default DockShellRow;
