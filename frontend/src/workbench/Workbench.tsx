import { memo, Suspense, useEffect, useMemo, useRef, useState } from "react";
import { Events, Window } from "@wailsio/runtime";
import { Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { Dock } from "./dock/Dock";
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
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Kbd } from "@/components/ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SplitPanelProvider, SplitPanelSlot } from "@/components/ui/split-panel";
import { useModuleActionsStore } from "../stores/moduleActions";
import { useWorkbenchTabsStore, type WorkbenchTab } from "../stores/workbenchTabs";
import { TabStrip } from "./tabs/TabStrip";
import { cn } from "@/lib/utils";

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

type WorkbenchViewsProps = {
  availableModules: ModuleManifest[];
  hasMetadataWarning: boolean;
  openTabs: WorkbenchTab[];
  activeTabId: string;
};

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
const WorkbenchViews = memo(function WorkbenchViews({
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
          <AlertDescription>Module metadata is unavailable or out of sync.</AlertDescription>
        </Alert>
      ) : null}
      <section
        className={cn(
          "flex min-h-0 flex-1 flex-col",
          activeModule?.id === "notes" ? "overflow-hidden p-0" : "overflow-auto p-4",
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
            const view = module?.views.find((candidate) => candidate.id === tab.viewId);
            if (!view) {
              return null;
            }
            const TabView = view.component;
            // Keep same-module inactive tabs mounted so in-progress edits
            // and scroll position survive switching between tabs of one
            // module, while `contents` preserves the active view's flex
            // participation without an extra layout wrapper.
            return (
              <div key={tab.id} className={tab.id === activeTabId ? "contents" : "hidden"}>
                <Suspense
                  fallback={
                    <div role="status" className="p-4 text-sm text-muted-foreground">
                      Loading {tab.title}...
                    </div>
                  }
                >
                  <TabView params={tab.params} />
                </Suspense>
              </div>
            );
          })
        )}
      </section>
    </div>
  );
});

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [switcher, setSwitcher] = useState<"closed" | "small" | "large">("closed");
  const [isFullscreen, setIsFullscreen] = useState(false);
  const moduleActions = useModuleActionsStore((s) => s.actions);
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
  const reorderTab = useWorkbenchTabsStore((state) => state.reorderTab);

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

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== "e" || !isTriggerModifier(event)) {
        return;
      }
      event.preventDefault();
      // First press (not already open) -> small popup. Any further "e"
      // while already open -> large — whether it's the OS's native
      // key-repeat from holding the key down, or a second discrete press
      // (the fallback for users with OS key-repeat disabled), both arrive
      // here as another keydown and take the same branch on purpose.
      setSwitcher((current) => (current === "closed" ? "small" : "large"));
    }

    function handleKeyUp(event: KeyboardEvent) {
      const isModifierKey =
        event.key === "Meta" || event.key === "Control" || event.key === "Shift";
      // Releasing either required modifier ends the session — cancel, not
      // commit (Enter is the only thing that switches tabs; see
      // TabSwitcher.tsx and the design spec for why this diverges from
      // ⌘Tab's release-to-commit).
      if (isModifierKey && !isTriggerModifier(event)) {
        setSwitcher("closed");
      }
    }

    window.addEventListener("keydown", handleKeyDown);
    window.addEventListener("keyup", handleKeyUp);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      window.removeEventListener("keyup", handleKeyUp);
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
      openTab({ moduleId: module.id, viewId: view.id, title: module.title });
    }
  }

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
        {/* The header shares the compact 30px row with AppKit's traffic lights.
            main.go intentionally leaves InvisibleTitleBarHeight at zero because
            Wails implements it as an unconditional native drag hit-zone. The
            explicit CSS handle below is the only draggable web region. */}
        {/* Keep the interactive titlebar no-drag by default. Wails listens for
            mousedown at window capture phase, before React/Motion can claim a
            tab gesture, so a draggable ancestor is too broad a fallback here.
            Only the empty traffic-light clearance is an explicit native window
            drag handle; tabs and controls can own their gestures reliably. */}
        <header
          className={cn(
            "relative flex h-[30px] shrink-0 items-center pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:no-drag]",
            isFullscreen ? "pl-1" : "pl-20",
          )}
        >
          <div
            aria-hidden="true"
            className={cn(
              "absolute inset-y-0 left-0 transition-[width] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:drag]",
              isFullscreen ? "w-1" : "w-20",
            )}
          />
          {/* 1fr/4fr/1fr, not flex-grow ratios: Grid's fr unit divides the
              full track width by ratio regardless of each column's own
              content size, which is what an even 1:4:1 split actually
              requires — flex-grow only distributes space left over after
              content sizing, so an empty left column wouldn't reliably end
              up the same width as a busy right column under flex.
              minmax(0,_) on every track, not bare fr: a bare `Nfr` track's
              implicit minimum is its content's min-content size, not 0 — with
              every tab in the strip set to shrink-0, that min-content size is
              large, so the middle track was blowing out past its fr share
              and eating the side columns' space entirely. minmax(0,Nfr) is
              the standard fix: it lets the fr ratio actually win. */}
          <div className="grid w-full grid-cols-[minmax(0,1fr)_minmax(0,4fr)_minmax(0,1fr)] items-center gap-2">
            {/* Status column — reserved layout space only. Content (agent
                status, index status, ...) isn't built yet. */}
            <div />
            <TabStrip
              tabs={openTabs}
              activeTabId={activeTabId}
              onSelect={setActiveTab}
              onClose={closeTab}
              onReorder={reorderTab}
            />
            <div className="flex min-w-0 items-center justify-end gap-2">
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
              {metadataLoading ? (
                <span
                  role="status"
                  className="shrink-0 text-xs text-muted-foreground [--wails-draggable:no-drag]"
                >
                  Loading module metadata...
                </span>
              ) : null}
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
                    onClick={() => focusModule("settings")}
                  >
                    <SettingsIcon className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Settings <Kbd>⌘,</Kbd>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        </header>
        {/* Transparent row: the main content card and the split panel are two
            independent bg-card boxes, separated by a gap that shows the same
            transparent/vibrancy background as the mx-1/mb-1 margins around
            this whole row — not one shared card with an internal divider. */}
        <div className="mx-1 mb-1 flex min-h-0 flex-1">
          <WorkbenchViews
            availableModules={availableModules}
            hasMetadataWarning={hasMetadataWarning}
            openTabs={openTabs}
            activeTabId={activeTabId}
          />
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
        <TabSwitcher
          open={switcher !== "closed"}
          size={switcher === "large" ? "large" : "small"}
          onClose={() => setSwitcher("closed")}
        />
        <InteractionHost />
      </div>
    </SplitPanelProvider>
  );
}
