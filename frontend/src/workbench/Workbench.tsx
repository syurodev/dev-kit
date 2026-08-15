import { useEffect, useMemo, useState } from "react";
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
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
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

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
  const [metadataLoading, setMetadataLoading] = useState(true);
  const [paletteOpen, setPaletteOpen] = useState(false);
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
      <div className="relative flex min-h-svh w-full flex-col bg-transparent">
        <AgentSessionsSubscriber />
        {/* main.go sets Mac.InvisibleTitleBarHeight: 50 — a draggable strip at
            the window top where the native traffic-light buttons sit. Rather
            than reserving that as empty canvas above a bordered card (wasted
            space), the header lives directly in this strip, on the canvas,
            padded left to clear the traffic lights — matching how native
            macOS toolbars combine both in one row. macOS-specific sizing for
            now (Windows/Linux titlebar chrome is a separate, deferred pass). */}
        {/* Header root defaults to draggable so empty space still moves the
            window (Wails needs this set explicitly — there is no built-in
            drag region under web content in hiddenInset mode). TabStrip and
            every button below override back to no-drag on themselves. */}
        <header className="flex h-[50px] shrink-0 items-center gap-2 pl-24 pr-4 [--wails-draggable:drag]">
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
                  size="icon-sm"
                  aria-label={action.label}
                  aria-pressed={action.isActive}
                  className={cn(
                    action.isActive && "bg-accent text-foreground",
                    "shrink-0 [--wails-draggable:no-drag]",
                  )}
                  onClick={action.onClick}
                >
                  <action.icon className="size-4" />
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
                size="icon-sm"
                aria-label="Commands"
                className="shrink-0 [--wails-draggable:no-drag]"
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
                variant="ghost"
                size="icon-sm"
                aria-label="Settings"
                className="shrink-0 [--wails-draggable:no-drag]"
                onClick={() => focusModule("settings")}
              >
                <SettingsIcon className="size-4" />
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
                "flex min-h-0 flex-1 flex-col overflow-auto",
                activeModule?.id === "notes" ? "p-0" : "p-4",
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
                  // `contents` (not a fragment) keeps the active view's own
                  // flex/overflow classes participating directly in this
                  // section's layout, exactly as the single <ActiveView/> did
                  // before. Inactive tabs stay mounted so in-progress state
                  // (a running query, an unsaved draft) survives switching away.
                  return (
                    <div key={tab.id} className={tab.id === activeTabId ? "contents" : "hidden"}>
                      <TabView params={tab.params} />
                    </div>
                  );
                })
              )}
            </section>
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
        <InteractionHost />
      </div>
    </SplitPanelProvider>
  );
}
