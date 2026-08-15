import { memo, useEffect, useMemo, useState } from "react";
import { Events, Window } from "@wailsio/runtime";
import { Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { Dock } from "./dock/Dock";
import { AgentSessionsSubscriber } from "./AgentSessionsSubscriber";
import { CommandPalette } from "./CommandPalette";
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
            const module = getModule(tab.moduleId);
            const view = module?.views.find((candidate) => candidate.id === tab.viewId);
            if (!view) {
              return null;
            }
            const TabView = view.component;
            // Keep inactive tabs mounted so running work and drafts survive
            // switching, while `contents` preserves the active view's flex
            // participation without an extra layout wrapper.
            return (
              <div key={tab.id} className={tab.id === activeTabId ? "contents" : "hidden"}>
                <TabView params={tab.params} />
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
  const [isFullscreen, setIsFullscreen] = useState(false);
  const moduleActions = useModuleActionsStore((s) => s.actions);
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
  const reorderTab = useWorkbenchTabsStore((state) => state.reorderTab);

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
            "relative flex h-[30px] shrink-0 items-center gap-2 pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:no-drag]",
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
          <TabStrip
            tabs={openTabs}
            activeTabId={activeTabId}
            onSelect={setActiveTab}
            onClose={closeTab}
            onReorder={reorderTab}
          />
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
        <InteractionHost />
      </div>
    </SplitPanelProvider>
  );
}
