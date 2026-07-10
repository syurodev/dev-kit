import { useMemo, useState } from "react";
import { ActivityBar } from "./ActivityBar";
import { CommandPalette } from "./CommandPalette";
import { listRegisteredModules, registerModule } from "../modules/registry";
import { toolsModule } from "../modules/tools/manifest";

registerModule(toolsModule);

export function Workbench() {
  const modules = useMemo(() => listRegisteredModules(), []);
  const [activeModuleId, setActiveModuleId] = useState(modules[0]?.id ?? "");
  const [paletteOpen, setPaletteOpen] = useState(false);

  const activeModule =
    modules.find((module) => module.id === activeModuleId) ?? modules[0];
  const activeView = activeModule?.views[0];
  const ActiveView = activeView?.component;
  const commands = modules.flatMap((module) => module.commands);

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
