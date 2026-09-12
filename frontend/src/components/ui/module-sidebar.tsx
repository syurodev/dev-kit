import { Suspense } from "react";
import { getModule } from "@/modules/registry";

/**
 * The rail a module has by being the module on screen.
 *
 * SidebarSurface prefers whatever a document registered — a rail that reflects
 * the open document needs to come from it — and falls back to here. Before
 * this, a module with nothing open had nothing registered and the rail went
 * blank, which is what "where is the sidebar" was.
 */
export function ModuleSidebar({ moduleId }: { moduleId: string }) {
  const Sidebar = getModule(moduleId)?.sidebar;
  if (!Sidebar) {
    return null;
  }
  return (
    <Suspense fallback={null}>
      <Sidebar />
    </Suspense>
  );
}
