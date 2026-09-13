import { lazy, memo, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { ModuleSkeleton } from "./ModuleSkeleton";
import { Events, Window } from "@wailsio/runtime";
import { MessageSquare, PanelBottom, PanelLeft, Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { useMenuCommands } from "./menuCommands";
import { ModuleEmptyState } from "./ModuleEmptyState";
import { Dock } from "./dock/Dock";
import { moduleIndexFromEvent } from "./dock/dockActivation";
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { CommandPalette } from "./CommandPalette";
import { SystemStatusMenu } from "./SystemStatusMenu";
import { TabStrip } from "./tabs/TabStrip";
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
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
import { useModuleActionsStore } from "../stores/moduleActions";
import {
  allOpenTabs,
  moduleTabs,
  useWorkbenchTabsStore,
  type WorkbenchTab,
} from "../stores/workbenchTabs";
import { useSidebarOpenStore } from "../stores/sidebarShell";
import { useBottomSlotStore } from "../stores/bottomSlot";
import { useAgentSessionsStore } from "../stores/agentSessions";
import { AppShell } from "./AppShell";
import { cn } from "@/lib/utils";
import { haveSameTabMembers } from "./workbenchViewMemo";

const LazyAgentPermissionToastHost = lazy(() =>
  import("./AgentPermissionToastHost").then((m) => ({
    default: m.AgentPermissionToastHost,
  })),
);

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
  /** Every module's tabs — this layer keeps recently-left modules mounted. */
  openTabs: WorkbenchTab[];
  /** The module on screen, and the active tab of that module. */
  activeModuleId: string;
  activeTabId: string;
};

function areWorkbenchViewsPropsEqual(
  previous: WorkbenchViewsProps,
  next: WorkbenchViewsProps,
): boolean {
  return (
    previous.availableModules === next.availableModules &&
    previous.hasMetadataWarning === next.hasMetadataWarning &&
    previous.activeModuleId === next.activeModuleId &&
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
    activeModuleId,
    activeTabId,
  }: WorkbenchViewsProps) {
    // The module is state now, not something inferred from whichever tab is
    // active: a module whose last tab was just closed is still the module on
    // screen, and inferring it from a tab would have flashed the first
    // module in the list for the frame before its home view reopens.
    const activeModule =
      availableModules.find((module) => module.id === activeModuleId) ??
      availableModules[0];
    const recentModuleIds = useMountedModuleIds(activeModuleId, HEADER_ONLY_MODULE_IDS);
    // The active module is unioned in directly (not only through
    // recentModuleIds) because useMountedModuleIds only learns about it via
    // an effect, which runs one render after activeTab already changed —
    // without this, the just-activated tab would render null for that one
    // frame. React Compiler memoizes this automatically; no manual useMemo
    // needed (and the previous one didn't work anyway — recentModuleIds is a
    // new Set identity every render, so its dependency never matched).
    const mountedModuleIds = new Set([
      ...HEADER_ONLY_MODULE_IDS,
      ...(activeModuleId ? [activeModuleId] : []),
      ...recentModuleIds,
    ]);

    return (
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {hasMetadataWarning ? (
          <Alert variant="error" className="m-4 mb-0 w-auto">
            <AlertDescription>
              Module metadata is unavailable or out of sync.
            </AlertDescription>
          </Alert>
        ) : null}
        <section
          className={cn(
            "flex min-h-0 flex-1 flex-col",
            activeModule?.padded
              ? // Half the gutter at the top when a tab strip is above, because
                // the strip already ends in 8px of its own. The full 16 made 18
                // between the tab and the first control that acts on it, and a
                // toolbar that far from its tab reads as a band of its own
                // rather than as that document's controls. Without a strip the
                // section touches the card's edge and owes the full gutter.
                activeModule.tabs
                ? "overflow-auto px-4 pb-4 pt-2"
                : "overflow-auto p-4"
              : "overflow-hidden p-0",
          )}
        >
          {/* `openTabs` is EVERY module's tabs — the views layer keeps recently
              left modules mounted. So "is anything open" has to be asked about
              the module on screen, not about the list: with a Notes tab open
              and no Database one, the old check fell through to the map, which
              rendered nothing for Database at all. A blank card.

              "No view is currently available" was also a status line for a
              state that is not a problem: nothing is open because nothing was
              left open. writing.md asks a blank screen to say what to do next
              and to give a control for doing it. */}
          {!openTabs.some((tab) => tab.moduleId === activeModule?.id) ? (
            <ModuleEmptyState module={activeModule} />
          ) : null}
          {openTabs.length === 0 ? null : (
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
                  {/* Provider outside Suspense so the skeleton can register
                      a rail under this module's id while the chunk loads. */}
                  <SidebarModuleIdContext.Provider value={tab.moduleId}>
                    <Suspense fallback={<ModuleSkeleton />}>
                      {/* module-enter mounts with the chunk, so its
                          @starting-style fires at the skeleton→content swap
                          and nowhere else. It is the flex box the `contents`
                          wrapper gave the module before. */}
                      <div className="module-enter">
                        <TabView params={tab.params} />
                      </div>
                    </Suspense>
                  </SidebarModuleIdContext.Provider>
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
  const chatOpen = useAgentSessionsStore((s) => s.chatOpen);
  const setChatOpen = useAgentSessionsStore((s) => s.setChatOpen);

  const moduleActions = useModuleActionsStore((s) => s.actions);
  const activeModuleId = useWorkbenchTabsStore((state) => state.activeModuleId);
  const tabModules = useWorkbenchTabsStore((state) => state.modules);
  // The strip and the switcher see one module's tabs; only the views layer
  // sees everyone's, because it keeps recently-left modules mounted. Memoised
  // on the map's identity so a reorder inside one module still reaches
  // haveSameTabMembers and is recognised as a no-op for the heavy subtree.
  const activeTabs = moduleTabs(tabModules, activeModuleId);
  const allTabs = useMemo(() => allOpenTabs(tabModules), [tabModules]);
  const focusModuleInStore = useWorkbenchTabsStore((state) => state.focusModule);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const reorderTab = useWorkbenchTabsStore((state) => state.reorderTab);
  const pinTab = useWorkbenchTabsStore((state) => state.pinTab);

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
  const activeModule =
    availableModules.find((module) => module.id === activeModuleId) ??
    availableModules[0];

  function focusModule(moduleId: string) {
    const module = getModule(moduleId);
    const view = module?.views[0];
    if (!module || !view) {
      return;
    }
    // Start the module request before the tab state update triggers render.
    // The same cached promise is consumed by React.lazy in WorkbenchViews.
    const preload = view.preload?.();
    void preload?.catch(() => undefined);
    focusModuleInStore(moduleId);
    // Sticky (module-tabs.mdx): a module that already has a tab active comes
    // back to exactly that tab — its tabs are its own and were never touched
    // by whatever happened in other modules meanwhile. Tabs are stored now, so
    // that also holds across a relaunch.
    //
    // A module with NOTHING open opens nothing. It used to get its home view
    // as a preview tab, which was a guess standing in for the memory the
    // workbench did not have: visiting Database always produced a blank
    // console whether or not anyone wanted one. The empty state below offers
    // it instead, which is the same click without the assumption.
  }

  // Mirrors for the mount-once keydown listener above, which must not close
  // over the first render's values. Declared here, after both definitions.
  const dockModulesRef = useRef(dockModules);
  dockModulesRef.current = dockModules;
  const focusModuleRef = useRef(focusModule);
  focusModuleRef.current = focusModule;

  useEffect(() => {
    if (availableModules.length === 0) {
      return;
    }
    if (!activeModuleId) {
      focusModule(availableModules[0].id);
      return;
    }
    // Closing a module's last tab empties its partition. The module stays on
    // screen — with the empty state, not a view conjured back — rather than
    // the app jumping to whichever module happens to be first in the list.
  }, [availableModules, activeModuleId]);
  useMenuCommands();

  const commands = availableModules.flatMap((module) => module.commands);
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    <AppShell
      fullscreen={isFullscreen}
      activeModuleId={activeModule?.id ?? null}
      dock={
        <Dock
          activeModuleId={activeModule?.id ?? ""}
          modules={dockModules}
          onSelectModule={focusModule}
        />
      }
      overlays={
        <>
          <AgentSessionsSubscriber />
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
          <Suspense fallback={null}>
            <LazyAgentPermissionToastHost activeModuleId={activeModule?.id ?? null} />
          </Suspense>
        </>
      }
      titlebar={
        <>
          {/* Sidebar toggle sits immediately after the traffic lights and
              deliberately OUTSIDE the three zones below — it is not a status
              item, a module action, or an app action, so it does not belong to
              any of them. pl-21 (84px) + mx-1 (4px) puts its left edge at
              x=88, matching sidebar-toggle's own x=88 in the App Shell
              component in Figma. The reservation used to be pl-20, which is
              80px and landed the control at 84 — the comment claimed 88 and
              the arithmetic did not. Traffic lights end at x=72, so 88 also
              reproduces Figma's 16px clearance. */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="titlebar"
                size="icon-sm"
                aria-label="Toggle sidebar"
                aria-pressed={sidebarOpen}
                className={cn(
                  sidebarOpen && "bg-accent text-foreground",
                  "mx-1",
                )}
                onClick={() => setSidebarOpen(!sidebarOpen)}
              >
                <PanelLeft className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">Toggle sidebar</TooltipContent>
          </Tooltip>
          {/* Two zones, not three. The workspace pill that used to hold the
              centre is gone: it named a workspace this app has no concept of,
              and carried an env chip for a value that belongs to a connection
              rather than to the window — the Database header already shows it
              beside the profile it describes. Module actions move left, where
              they sit next to the content they act on; everything the shell
              owns collects on the right. */}
          <div className="flex h-full min-w-0 flex-1 items-center gap-1">
            <div className="flex min-w-0 items-center gap-1">
              {moduleActions.map((action) => (
                <Tooltip key={action.id}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="titlebar"
                      size="icon-sm"
                      aria-label={action.label}
                      aria-pressed={action.isActive}
                      className={cn(
                        action.isActive && "bg-accent text-foreground",
                      )}
                      onClick={action.onClick}
                    >
                      <action.icon className="size-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">{action.label}</TooltipContent>
                </Tooltip>
              ))}
            </div>
            <div className="min-w-0 flex-1 select-none truncate text-caption-11 text-text-tertiary">
              {metadataLoading ? "Loading module metadata..." : null}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <SystemStatusMenu
                onResolveConflicts={() => openTab({ moduleId: "sync", viewId: "sync.status", title: "Sync" })}
                onOpenAgents={() => setChatOpen(true)}
              />
              <span className="mx-1 h-4 w-px shrink-0 bg-border" aria-hidden />
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Commands"
                    onClick={() => setPaletteOpen(true)}
                  >
                    <Search className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Commands <Kbd>⌘K</Kbd>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Settings"
                    onClick={() => setSettingsOpen(true)}
                  >
                    <SettingsIcon className="size-4" />
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
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Toggle bottom panel"
                    aria-pressed={bottomOpen}
                    className={cn(
                      bottomOpen && "bg-accent text-foreground",
                    )}
                    onClick={() => setBottomOpen(!bottomOpen)}
                  >
                    <PanelBottom className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle bottom panel</TooltipContent>
              </Tooltip>

              {/* Always visible, like the bottom-panel toggle beside it: the
                  conversation belongs to the shell, so its control cannot
                  appear and disappear with the active module. */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="titlebar"
                    size="icon-sm"
                    aria-label="Toggle agent conversation"
                    aria-pressed={chatOpen}
                    className={cn(
                      chatOpen && "bg-accent text-foreground",
                    )}
                    onClick={() => setChatOpen(!chatOpen)}
                  >
                    <MessageSquare className="size-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle agent conversation</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </>
      }
    >
      {/* The strip belongs to the module on screen and shows only its tabs.
          It is drawn only for modules that opted in (ModuleManifest.tabs):
          a module with a single view has nothing to switch between, and the
          48px would sit under the 40px title bar as a second empty band —
          which is exactly what read as "the title bar is too tall". */}
      {activeModule?.tabs ? (
        <TabStrip
          tabs={activeTabs.openTabs}
          activeTabId={activeTabs.activeTabId}
          onSelect={setActiveTab}
          onClose={closeTab}
          onReorder={reorderTab}
          onPin={pinTab}
          onNewTab={activeModule?.newTab}
          keyboardBrowsing={switcher === "browsing"}
          onExitBrowsing={() => setSwitcher("closed")}
        />
      ) : null}
      <WorkbenchViews
        availableModules={availableModules}
        hasMetadataWarning={hasMetadataWarning}
        openTabs={allTabs}
        activeModuleId={activeModule?.id ?? ""}
        activeTabId={activeTabs.activeTabId}
      />
    </AppShell>
  );
}
