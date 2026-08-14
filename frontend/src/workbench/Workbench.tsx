import { useEffect, useMemo, useState } from "react";
import { Bot, Search, Settings as SettingsIcon } from "lucide-react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { Dock } from "./dock/Dock";
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
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Kbd } from "@/components/ui/kbd";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
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
  const dockModules = useMemo(
    () => activityModules.filter((module) => !HEADER_ONLY_MODULE_IDS.has(module.id)),
    [activityModules],
  );
  const activeModule =
    availableModules.find((module) => module.id === activeModuleId) ??
    availableModules[0];
  const activeView = activeModule?.views[0];
  const ActiveView = activeView?.component;
  const commands = availableModules.flatMap((module) => module.commands);
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    // Transparent, not bg-background: this outer frame is what the native
    // macOS vibrancy blur (Mac.Backdrop in main.go) needs to show through.
    // Only the content card below (bg-card) stays opaque.
    <div className="relative flex min-h-svh w-full flex-col bg-transparent">
      <AgentSessionsSubscriber />
      {/* main.go sets Mac.InvisibleTitleBarHeight: 50 — a draggable strip at
          the window top where the native traffic-light buttons sit. Rather
          than reserving that as empty canvas above a bordered card (wasted
          space), the header lives directly in this strip, on the canvas,
          padded left to clear the traffic lights — matching how native
          macOS toolbars combine both in one row. macOS-specific sizing for
          now (Windows/Linux titlebar chrome is a separate, deferred pass). */}
      <header className="flex h-[50px] shrink-0 items-center gap-2 pl-24 pr-4">
        <div className="flex size-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
          <Bot className="size-3.5" />
        </div>
        <strong className="text-sm font-semibold">
          {activeModule?.title ?? "DevKit"}
        </strong>
        {metadataLoading ? (
          <span role="status" className="text-xs text-muted-foreground">
            Loading module metadata...
          </span>
        ) : null}
        <div className="flex-1" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
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
              variant="ghost"
              size="icon-sm"
              aria-label="Settings"
              onClick={() => setActiveModuleId("settings")}
            >
              <SettingsIcon className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            Settings <Kbd>⌘,</Kbd>
          </TooltipContent>
        </Tooltip>
      </header>
      <div className="mx-2 mb-2 flex flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
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
      </div>
      <Dock
        activeModuleId={activeModule?.id ?? ""}
        modules={dockModules}
        onSelectModule={setActiveModuleId}
      />
      <CommandPalette
        open={paletteOpen}
        commands={commands}
        onClose={() => setPaletteOpen(false)}
      />
      <InteractionHost />
    </div>
  );
}
