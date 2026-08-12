import { useEffect, useMemo, useState } from "react";
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
    // Transparent, not bg-background: this outer frame is what the native
    // macOS vibrancy blur (Mac.Backdrop in main.go) needs to show through.
    // Only the content card below (bg-card) stays opaque.
    <div className="relative flex min-h-svh w-full flex-col bg-transparent">
      <AgentSessionsSubscriber />
      {/* mt-[58px]: main.go sets Mac.InvisibleTitleBarHeight: 50 — that
          reserves a draggable strip at the window top where the native
          traffic-light buttons sit. The floating dock (no sidebar to carry
          this concern anymore) has no such collision, so only the content
          card needs the clearance; the extra 8px matches the mx/mb gap on
          the other three sides. macOS-specific for now (Windows/Linux
          titlebar chrome is a separate, deliberately deferred pass). */}
      <div className="mx-2 mb-2 mt-[58px] flex flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
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
      </div>
      <Dock
        activeModuleId={activeModule?.id ?? ""}
        modules={activityModules}
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
