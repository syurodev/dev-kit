import { Plus } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { Button } from "@/components/ui/button";
import type { ModuleManifest } from "@/modules/registry";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * A module with nothing open.
 *
 * Reached two ways, and they mean the same thing: the last tab was closed, or
 * the workbench came back with nothing stored for this module. The old copy —
 * "No view is currently available." — reported it as a fault, and it is not
 * one; it is an empty desk with the drawer right there.
 */
export function ModuleEmptyState({ module }: { module: ModuleManifest | undefined }) {
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const home = module?.views[0];
  if (!module || !home) {
    return (
      <EmptyState
        icon={Plus}
        title="No modules are available"
        body="Nothing is registered to show here yet."
      />
    );
  }
  return (
    <EmptyState
      icon={module.icon}
      title={`${module.title} has nothing open`}
      body={`Open ${home.title.toLowerCase()} to start, or use the tab strip's + button.`}
    >
      <Button
        size="sm"
        onClick={() =>
          // The module's own newTab where it has one — the strip's + and this
          // button must not disagree about what "a new one" means.
          module.newTab
            ? module.newTab()
            : openTab({
                moduleId: module.id,
                viewId: home.id,
                title: module.title,
                icon: home.icon,
              })
        }
      >
        <Plus />
        Open {home.title.toLowerCase()}
      </Button>
    </EmptyState>
  );
}
