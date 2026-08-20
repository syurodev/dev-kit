import { lazy, memo, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { Events, Window } from "@wailsio/runtime";
import { PanelBottom, PanelLeft, Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { Dock } from "./dock/Dock";
import { moduleIndexFromEvent } from "./dock/dockActivation";
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { CommandPalette } from "./CommandPalette";
import { TabSwitcher } from "./TabSwitcher";
import { InteractionHost } from "./InteractionHost";
import {
  compareModuleParity,
  CURRENT_MODULE_API_VERSION,
  getModule,
  listRegisteredModules,
  registerModule,
  type ModuleManifest,
} from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";
import { notesModule } from "../modules/notes/manifest";
import { databaseModule } from "../modules/database/manifest";
import { sshModule } from "../modules/ssh/manifest";
import { kafkaModule } from "../modules/kafka/manifest";
import { agenthubModule } from "../modules/agenthub/manifest";
import { syncModule } from "../modules/sync/manifest";
import { settingsModule } from "../modules/settings/manifest";
import { codegraphModule } from "../modules/codegraph/manifest";

const SettingsModal = lazy(() =>
  import("../modules/settings/SettingsView").then((module) => ({
    default: module.SettingsModal,
  })),
);
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Kbd } from "@/components/ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SplitPanelProvider, SplitPanelSlot } from "@/components/ui/split-panel";
import { SidebarSurface, SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import { BottomSlotSurface } from "@/components/ui/bottom-slot";
import { useModuleActionsStore } from "../stores/moduleActions";
import { useWorkbenchTabsStore, type WorkbenchTab } from "../stores/workbenchTabs";
import { useSidebarOpenStore } from "../stores/sidebarShell";
import { useBottomSlotStore } from "../stores/bottomSlot";
import { cn } from "@/lib/utils";
import { haveSameTabMembers } from "./workbenchViewMemo";

// Settings absorbs Sync as a tab (SettingsView) — both stay registered
// modules (backend still reports "sync", so removing its registration
// would trip the metadata-parity check), they just don't get their own
// dock icon anymore.
const HEADER_ONLY_MODULE_IDS = new Set(["settings", "sync"]);

// Bounds how many modules' tab content stays mounted in WorkbenchViews:
// the active module plus up to MAX_MOUNTED_MODULES - 1 more that were
// active within the last MODULE_GRACE_MS. See useMountedModuleIds below.
const MODULE_GRACE_MS = 2 * 60 * 1000;
const MAX_MOUNTED_MODULES = 3;

registerModule(toolsModule);
registerModule(notesModule);
registerModule(databaseModule);
registerModule(sshModule);
registerModule(kafkaModule);
registerModule(agenthubModule);
registerModule(syncModule);
registerModule(settingsModule);
registerModule(codegraphModule);

type WorkbenchViewsProps = {
  availableModules: ModuleManifest[];
  hasMetadataWarning: boolean;
  openTabs: WorkbenchTab[];
  activeTabId: string;
};

function areWorkbenchViewsPropsEqual(
  previous: WorkbenchViewsProps,
  next: WorkbenchViewsProps,
): boolean {
  return (
    previous.availableModules === next.availableModules &&
    previous.hasMetadataWarning === next.hasMetadataWarning &&
    previous.activeTabId === next.activeTabId &&
    haveSameTabMembers(previous.openTabs, next.openTabs)
  );
}

/**
 * MRU-tracked set of non-exempt module ids allowed to stay mounted: the
 * active module always qualifies; up to MAX_MOUNTED_MODULES - 1 more
 * recently-active modules stay mounted for MODULE_GRACE_MS after being
 * left. Exempt ids (HEADER_ONLY_MODULE_IDS) are never added here — the
 * caller unions them in separately and they're always mounted regardless
 * of this hook's output.
 */
function useMountedModuleIds(
  activeModuleId: string,
  exemptIds: ReadonlySet<string>,
): Set<string> {
  const [recent, setRecent] = useState<string[]>([]);
  const prevActive = useRef("");
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (!activeModuleId) {
      return;
    }
    const leaving = prevActive.current;
    prevActive.current = activeModuleId;

    if (leaving && leaving !== activeModuleId && !exemptIds.has(leaving)) {
      const timer = setTimeout(() => {
        timers.current.delete(leaving);
        setRecent((prev) => prev.filter((id) => id !== leaving));
      }, MODULE_GRACE_MS);
      timers.current.set(leaving, timer);
    }

    if (exemptIds.has(activeModuleId)) {
      return;
    }

    const ownTimer = timers.current.get(activeModuleId);
    if (ownTimer) {
      clearTimeout(ownTimer);
      timers.current.delete(activeModuleId);
    }

    setRecent((prev) => {
      const next = [activeModuleId, ...prev.filter((id) => id !== activeModuleId)];
      for (const evictedId of next.slice(MAX_MOUNTED_MODULES)) {
        const evictedTimer = timers.current.get(evictedId);
        if (evictedTimer) {
          clearTimeout(evictedTimer);
          timers.current.delete(evictedId);
        }
      }
      return next.slice(0, MAX_MOUNTED_MODULES);
    });
  }, [activeModuleId, exemptIds]);

  useEffect(() => {
    const timerMap = timers.current;
    return () => {
      for (const timer of timerMap.values()) {
        clearTimeout(timer);
      }
    };
  }, []);

  return new Set(recent);
}

/**
 * Heavy module trees are deliberately isolated from tab-strip ordering.
 * Tab dragging changes only TabStrip-local state and commits to the store once
 * on drop, so this subtree stays completely outside the high-frequency path.
 * The explicit props keep tab activation atomic: preview replacement and its
 * active id are rendered from the same Zustand snapshot.
 */
const WorkbenchViews = memo(
  function WorkbenchViews({
    availableModules,
    hasMetadataWarning,
    openTabs,
    activeTabId,
  }: WorkbenchViewsProps) {
    const activeTab = openTabs.find((tab) => tab.id === activeTabId);
    const activeModule =
      availableModules.find((module) => module.id === activeTab?.moduleId) ??
      availableModules[0];
    const recentModuleIds = useMountedModuleIds(
      activeTab?.moduleId ?? "",
      HEADER_ONLY_MODULE_IDS,
    );
    // The active module is unioned in directly (not only through
    // recentModuleIds) because useMountedModuleIds only learns about it via
    // an effect, which runs one render after activeTab already changed —
    // without this, the just-activated tab would render null for that one
    // frame. React Compiler memoizes this automatically; no manual useMemo
    // needed (and the previous one didn't work anyway — recentModuleIds is a
    // new Set identity every render, so its dependency never matched).
    const mountedModuleIds = new Set([
      ...HEADER_ONLY_MODULE_IDS,
      ...(activeTab ? [activeTab.moduleId] : []),
      ...recentModuleIds,
    ]);

    return (
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
        {hasMetadataWarning ? (
          <Alert variant="destructive" className="m-4 mb-0 w-auto">
            <AlertDescription>
              Module metadata is unavailable or out of sync.
            </AlertDescription>
          </Alert>
        ) : null}
        <section
          className={cn(
            "flex min-h-0 flex-1 flex-col",
            activeModule?.id === "notes"
              ? "overflow-hidden p-0"
              : "overflow-auto p-4",
          )}
        >
          {openTabs.length === 0 ? (
            <p role="status" className="text-sm text-muted-foreground">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          ) : (
            openTabs.map((tab) => {
              // Tabs outside the active module's grace/cap window aren't
              // mounted at all — see useMountedModuleIds above. Their tab
              // stays in openTabs (visible, clickable) and remounts fresh
              // when reactivated; background work belonging to that module
              // (e.g. Agent Hub sessions) lives outside this render tree and
              // is unaffected either way.
              if (!mountedModuleIds.has(tab.moduleId)) {
                return null;
              }
              const module = getModule(tab.moduleId);
              const view = module?.views.find(
                (candidate) => candidate.id === tab.viewId,
              );
              if (!view) {
                return null;
              }
              const TabView = view.component;
              // Keep same-module inactive tabs mounted so in-progress edits
              // and scroll position survive switching between tabs of one
              // module, while `contents` preserves the active view's flex
              // participation without an extra layout wrapper.
              return (
                <div
                  key={tab.id}
                  className={tab.id === activeTabId ? "contents" : "hidden"}
                >
                  <Suspense
                    fallback={
                      <div
                        role="status"
                        className="p-4 text-sm text-muted-foreground"
                      >
                        Loading {tab.title}...
                      </div>
                    }
                  >
                    <SidebarModuleIdContext.Provider value={tab.moduleId}>
                      <TabView params={tab.params} />
                    </SidebarModuleIdContext.Provider>
                  </Suspense>
                </div>
              );
            })
          )}
        </section>
      </div>
    );
  },
  areWorkbenchViewsPropsEqual,
);

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // "browsing": the real tab strip is forced to full width, ArrowLeft/
  // ArrowRight + Enter navigate it directly (TabStrip.tsx) — the first
  // ⌘⇧E press. "popup": the detail popup (TabSwitcher.tsx) — reached by
  // holding the key down or pressing it again while already browsing.
  const [switcher, setSwitcher] = useState<"closed" | "browsing" | "popup">("closed");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const sidebarOpen = useSidebarOpenStore((s) => s.open);
  const setSidebarOpen = useSidebarOpenStore((s) => s.setOpen);
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);
  const moduleActions = useModuleActionsStore((s) => s.actions);
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);

  useEffect(() => {
    // Accept either ⌘ or Ctrl rather than branching on navigator.platform:
    // that value is unreliable inside Wails' WKWebView (empty or
    // inconsistent depending on build/runtime config), which made this
    // check on macOS test event.ctrlKey — the wrong modifier for a real
    // ⌘⇧E press — so the shortcut silently never fired. Checking the
    // event's actual modifier state directly needs no platform guess at
    // all.
    function isTriggerModifier(event: KeyboardEvent): boolean {
      return (event.metaKey || event.ctrlKey) && event.shiftKey;
    }

    // A single, ordinary tap of a key isn't instantaneous — the press can
    // last past the OS's key-repeat delay on some systems, which fired a
    // second "browsing already active" keydown and escalated straight to
    // the popup on what the user experienced as one press. Requiring
    // browsing to have been active for at least this long before a further
    // "e" is allowed to escalate distinguishes an actual hold from that.
    const ESCALATE_HOLD_MS = 400;
    let browsingStartedAt = 0;

    function handleKeyDown(event: KeyboardEvent) {
      // ⌘1–⌘9 select the nth dock module. Read through refs because this
      // listener is registered once (empty deps) and must never see a
      // stale module list — same reason stores/moduleActions.ts reads
      // onClick through a ref inside its long-lived registration.
      const moduleIndex = moduleIndexFromEvent(event);
      if (moduleIndex !== null) {
        const target = dockModulesRef.current[moduleIndex];
        if (target && !target.unavailable) {
          event.preventDefault();
          focusModuleRef.current(target.id);
        }
        // preventDefault stays inside the branch above: an unusable index
        // (⌘8 with seven modules) has no default worth suppressing — the
        // menu Wails installs binds ⌘0 but no ⌘1–⌘9, and ⌘0 never gets
        // here — so leave it for whatever else might want it.
        return;
      }
      if ((event.metaKey || event.ctrlKey) && event.key === ",") {
        event.preventDefault();
        setSettingsOpen((current) => !current);
        return;
      }
      if (event.key.toLowerCase() !== "e" || !isTriggerModifier(event)) {
        return;
      }
      event.preventDefault();
      setSwitcher((current) => {
        if (current === "closed") {
          browsingStartedAt = Date.now();
          return "browsing";
        }
        if (current === "browsing" && Date.now() - browsingStartedAt >= ESCALATE_HOLD_MS) {
          // Whether this "e" is the OS's native key-repeat from holding
          // the key down, or a second discrete press (the fallback for
          // users with OS key-repeat disabled), both arrive here as
          // another keydown and take the same branch on purpose.
          return "popup";
        }
        return current;
      });
    }

    // Neither state cares about the chord being released once entered —
    // browsing (like the popup) stays put after a plain press-and-release,
    // since requiring the user to keep three keys held down while also
    // pressing arrow keys/Enter to actually use it would be the opposite
    // of the point. Exiting is explicit: Enter selects, Escape cancels
    // (see TabStrip.tsx's own keydown handling, and the popup's own
    // Escape/click-outside handling in TabSwitcher.tsx via Dialog).
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, []);

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

  useEffect(() => {
    let mounted = true;
    let receivedFullscreenEvent = false;

    const updateFullscreen = (fullscreen: boolean) => {
      receivedFullscreenEvent = true;
      setIsFullscreen(fullscreen);
    };

    // The macOS "will" events start the web transition alongside the native
    // fullscreen animation. Common events remain as the cross-platform
    // fallback and are harmless when macOS emits them again on completion.
    const offWillFullscreen = Events.On(Events.Types.Mac.WindowWillEnterFullScreen, () => {
      updateFullscreen(true);
    });
    const offWillUnFullscreen = Events.On(Events.Types.Mac.WindowWillExitFullScreen, () => {
      updateFullscreen(false);
    });
    const offFullscreen = Events.On(Events.Types.Common.WindowFullscreen, () => {
      updateFullscreen(true);
    });
    const offUnFullscreen = Events.On(Events.Types.Common.WindowUnFullscreen, () => {
      updateFullscreen(false);
    });

    void Window.IsFullscreen()
      .then((fullscreen) => {
        if (mounted && !receivedFullscreenEvent) {
          setIsFullscreen(fullscreen);
        }
      })
      .catch(() => {
        // Browser-only development has no native window to query.
      });

    return () => {
      mounted = false;
      offWillFullscreen();
      offWillUnFullscreen();
      offFullscreen();
      offUnFullscreen();
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
  const dockModules = useMemo(
    () => activityModules.filter((module) => !HEADER_ONLY_MODULE_IDS.has(module.id)),
    [activityModules],
  );
  const activeTab = openTabs.find((tab) => tab.id === activeTabId);
  const activeModule =
    availableModules.find((module) => module.id === activeTab?.moduleId) ??
    availableModules[0];

  function focusModule(moduleId: string) {
    const module = getModule(moduleId);
    const view = module?.views[0];
    if (module && view) {
      // Start the module request before the tab state update triggers render.
      // The same cached promise is consumed by React.lazy in WorkbenchViews.
      const preload = view.preload?.();
      void preload?.catch(() => undefined);
      // Every module's home view opens as the one shared preview tab, not
      // a permanent tab of its own — casually visiting a module via the
      // Dock (without pinning anything inside it) shouldn't leave a tab
      // behind once you move on to another module. Confirmed explicitly:
      // this applies uniformly, not just to modules with a list-then-item
      // structure — switching from Notes to SSH via the Dock must replace
      // Notes' tab, not leave it stranded alongside a new permanent SSH
      // one. Picking a specific item inside a module (Notes: a note) still
      // replaces this same preview slot in place via openTab's existing
      // logic; a module's own view can still promote it to permanent
      // later (Notes does, on first edit, via pinTab) if that's ever
      // wired up for other modules too.
      openTab({
        moduleId: module.id,
        viewId: view.id,
        title: module.title,
        preview: true,
      });
    }
  }

  // Mirrors for the mount-once keydown listener above, which must not close
  // over the first render's values. Declared here, after both definitions.
  const dockModulesRef = useRef(dockModules);
  dockModulesRef.current = dockModules;
  const focusModuleRef = useRef(focusModule);
  focusModuleRef.current = focusModule;

  useEffect(() => {
    if (openTabs.length === 0 && availableModules.length > 0) {
      focusModule(availableModules[0].id);
    }
  }, [availableModules, openTabs.length]);
  const commands = availableModules.flatMap((module) => module.commands);
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    <SplitPanelProvider>
      {/* Transparent, not bg-background: this outer frame is what the native
          macOS vibrancy blur (Mac.Backdrop in main.go) needs to show through.
          Only the content card below (bg-card) stays opaque. */}
      <div className="relative flex h-svh w-full flex-col overflow-hidden bg-transparent">
        <AgentSessionsSubscriber />
        {/* The whole compact row is the window drag surface. Interactive
            module/global actions opt out individually with no-drag so Wails
            leaves their pointer events to the webview. main.go deliberately
            keeps InvisibleTitleBarHeight at zero: that native hit-zone cannot
            make the same child-level distinction. */}
        <header
          className={cn(
            "relative flex h-[30px] shrink-0 items-center pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:drag]",
            isFullscreen ? "pl-1" : "pl-20",
          )}
        >
          {/* Sidebar toggle sits immediately after the traffic lights and
              deliberately OUTSIDE the three zones below — it is not a status
              item, a module action, or an app action, so it does not belong to
              any of them. mx-2 lands it at x=88 against the header's pl-20
              (80px) traffic-light reservation, matching sidebar-toggle's own
              x=88 in the App Shell component in Figma. */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Toggle sidebar"
                aria-pressed={sidebarOpen}
                className={cn(
                  sidebarOpen && "bg-accent text-foreground",
                  "mx-2 shrink-0 [--wails-draggable:no-drag]",
                )}
                onClick={() => setSidebarOpen(!sidebarOpen)}
              >
                <PanelLeft className="size-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
          </Tooltip>
          {/* 1fr/5fr/1fr, not flex-grow ratios: Grid's fr unit divides the
              full track width by ratio regardless of each column's own
              content size, which is what an even 1:5:1 split actually
              requires — flex-grow only distributes space left over after
              content sizing, so an empty left column wouldn't reliably end
              up the same width as a busy right column under flex.
              minmax(0,_) on every track keeps long status/action content from
              expanding one region into its siblings. */}
          <div className="grid h-full min-w-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(0,5fr)_minmax(0,1fr)] items-center gap-2">
            <div className="min-w-0 select-none truncate text-xs text-muted-foreground">
              {metadataLoading ? "Loading module metadata..." : null}
            </div>
            <div className="flex min-w-0 items-center justify-center gap-2">
              {moduleActions.map((action) => (
                <Tooltip key={action.id}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={action.label}
                      aria-pressed={action.isActive}
                      className={cn(
                        action.isActive && "bg-accent text-foreground",
                        "shrink-0 [--wails-draggable:no-drag]",
                      )}
                      onClick={action.onClick}
                    >
                      <action.icon className="size-3.5" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">{action.label}</TooltipContent>
                </Tooltip>
              ))}
            </div>
            <div className="flex min-w-0 items-center justify-end gap-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Commands"
                    className="shrink-0 [--wails-draggable:no-drag]"
                    onClick={() => setPaletteOpen(true)}
                  >
                    <Search className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Commands <Kbd>⌘K</Kbd>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Settings"
                    className="shrink-0 [--wails-draggable:no-drag]"
                    onClick={() => setSettingsOpen(true)}
                  >
                    <SettingsIcon className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Settings <Kbd>⌘,</Kbd>
                </TooltipContent>
              </Tooltip>
              {/* Zone 3, after Settings — the rightmost item in the title bar.
                  The sidebar toggle is pinned at the far left, outside the
                  three zones, because Figma's App Shell puts it there against
                  the traffic lights; this one has no such constraint, so the
                  two layout toggles bracket the bar at its outer edges.

                  Always rendered and always enabled, even when the active
                  module contributes no panel: it toggles the surface, and the
                  surface explains its own emptiness. Disabling it would make
                  the control flicker in and out as the user changes module. */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Toggle bottom panel"
                    aria-pressed={bottomOpen}
                    className={cn(
                      bottomOpen && "bg-accent text-foreground",
                      "shrink-0 [--wails-draggable:no-drag]",
                    )}
                    onClick={() => setBottomOpen(!bottomOpen)}
                  >
                    <PanelBottom className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle bottom panel</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </header>
        {/* Transparent row: the main content card and the split panel are two
            independent bg-card boxes, separated by a gap that shows the same
            transparent/vibrancy background as the mx-1/mb-1 margins around
            this whole row — not one shared card with an internal divider. */}
        <div className="mx-1 mb-1 flex min-h-0 flex-1">
          <SidebarSurface activeModuleId={activeModule?.id ?? null} />
          {/* The bottom slot is stacked under the module content ONLY — the
              sidebar and the split panel keep their full height either side of
              it, so opening it never shortens them. min-w-0 because this
              column has no overflow-hidden of its own to zero its automatic
              minimum width, and without it the two side panels could not
              squeeze it. */}
          <div className="flex min-w-0 min-h-0 flex-1 flex-col">
            <WorkbenchViews
              availableModules={availableModules}
              hasMetadataWarning={hasMetadataWarning}
              openTabs={openTabs}
              activeTabId={activeTabId}
            />
            <BottomSlotSurface activeModuleId={activeModule?.id ?? null} />
          </div>
          <SplitPanelSlot />
        </div>
        <Dock
          activeModuleId={activeModule?.id ?? ""}
          modules={dockModules}
          onSelectModule={focusModule}
        />
        <CommandPalette
          open={paletteOpen}
          commands={commands}
          onClose={() => setPaletteOpen(false)}
        />
        <Suspense fallback={null}>
          <SettingsModal
            open={settingsOpen}
            onClose={() => setSettingsOpen(false)}
          />
        </Suspense>
        <TabSwitcher open={switcher === "popup"} onClose={() => setSwitcher("closed")} />
        <InteractionHost />
      </div>
    </SplitPanelProvider>
  );
}
