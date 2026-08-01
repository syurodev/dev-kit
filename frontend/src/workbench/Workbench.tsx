import { useEffect, useMemo, useState } from "react";
import { listModules, type ModuleDTO } from "../bridge/commands";
import { ActivityBar } from "./ActivityBar";
import { CommandPalette } from "./CommandPalette";
import {
  compareModuleParity,
  listRegisteredModules,
  registerModule,
} from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";
import { notesModule } from "../modules/notes/manifest";
import { databaseModule } from "../modules/database/manifest";
import { sshModule } from "../modules/ssh/manifest";

registerModule(toolsModule);
registerModule(notesModule);
registerModule(databaseModule);
registerModule(sshModule);

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [backendModules, setBackendModules] = useState<ModuleDTO[] | null>(null);
  const [metadataError, setMetadataError] = useState(false);
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
      });

    return () => {
      mounted = false;
    };
  }, []);

  const activeModule =
    modules.find((module) => module.id === activeModuleId) ?? modules[0];
  const activeView = activeModule?.views[0];
  const ActiveView = activeView?.component;
  const commands = modules.flatMap((module) => module.commands);
  const parityIssues = useMemo(
    () =>
      backendModules
        ? compareModuleParity(modules, backendModules)
        : [],
    [backendModules, modules],
  );
  const hasMetadataWarning = metadataError || parityIssues.length > 0;

  return (
    <main className="workbench">
      <ActivityBar
        activeModuleId={activeModule?.id ?? ""}
        modules={modules}
        onSelectModule={setActiveModuleId}
      />
      <section className="workbench-main">
        <header className="workbench-header">
          <strong>{activeModule?.title ?? "DevKit"}</strong>
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
          {ActiveView ? <ActiveView /> : null}
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
