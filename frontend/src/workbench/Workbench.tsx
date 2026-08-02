import { useEffect, useMemo, useState } from "react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { ActivityBar } from "./ActivityBar";
import { CommandPalette } from "./CommandPalette";
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
import { syncModule } from "../modules/sync/manifest";

registerModule(toolsModule);
registerModule(notesModule);
registerModule(databaseModule);
registerModule(sshModule);
registerModule(syncModule);

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
    () =>
      backendModules
        ? compareModuleParity(modules, backendModules)
        : [],
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
      if (
        metadata.apiVersion !== CURRENT_MODULE_API_VERSION &&
        metadata.id
      ) {
        unavailable.add(metadata.id);
      }
    }
    return unavailable;
  }, [backendModules, metadataError, parityIssues]);
  const availableModules = useMemo(
    () =>
      modules.filter((module) => !unavailableModuleIds.has(module.id)),
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
    <main className="workbench">
      <ActivityBar
        activeModuleId={activeModule?.id ?? ""}
        modules={activityModules}
        onSelectModule={setActiveModuleId}
      />
      <section className="workbench-main">
        <header className="workbench-header">
          <strong>{activeModule?.title ?? "DevKit"}</strong>
          {metadataLoading ? (
            <span role="status">Loading module metadata...</span>
          ) : null}
          {hasMetadataWarning ? (
            <span role="status">
              Module metadata is unavailable or out of sync.
            </span>
          ) : null}
          <button type="button" onClick={() => setPaletteOpen(true)}>
            Commands
          </button>
        </header>
        <section className="workbench-content">
          {ActiveView ? (
            <ActiveView />
          ) : (
            <p role="status">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          )}
        </section>
      </section>
      <CommandPalette
        open={paletteOpen}
        commands={commands}
        onClose={() => setPaletteOpen(false)}
      />
    </main>
  );
}
