import { Wrench } from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { getModule } from "../modules/registry";
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";

type TabSwitcherProps = {
  open: boolean;
  size: "small" | "large";
  onClose: () => void;
};

/**
 * Keyboard-driven tab switcher (⌘⇧E / Ctrl⇧E — see Workbench.tsx's
 * keydown/keyup handling for the escalating-chord detection). MRU order,
 * not the tab strip's own left-to-right/module-clustered order, since the
 * whole point is jumping back to whatever was just being looked at.
 *
 * No CommandInput (unlike CommandPalette): releasing either modifier key
 * closes this popup (see Workbench.tsx), which means there's never a
 * window where the user could actually type a search term without also
 * closing it — showing a search box that can't practically be used would
 * be misleading. Arrow keys + Enter over CommandList/CommandItem work
 * without an Input present; cmdk focuses the first item automatically.
 */
export function TabSwitcher({ open, size, onClose }: TabSwitcherProps) {
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const tabMru = useWorkbenchTabsStore((state) => state.tabMru);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);

  const byId = new Map(openTabs.map((tab) => [tab.id, tab]));
  const orderedIds = [
    ...tabMru.filter((id) => id !== activeTabId && byId.has(id)),
    // Tabs never activated yet (no MRU entry — e.g. opened via preview and
    // never focused) fall back to their openTabs position, appended after
    // the MRU-known ones rather than dropped from the list entirely.
    ...openTabs
      .map((tab) => tab.id)
      .filter((id) => id !== activeTabId && !tabMru.includes(id)),
  ];

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
      title="Switch tab"
      description="Jump to a recently used tab"
      className={size === "large" ? "sm:max-w-2xl" : "sm:max-w-md"}
    >
      <CommandList>
        <CommandEmpty>No other tabs are open.</CommandEmpty>
        <CommandGroup heading="Recent tabs">
          {orderedIds.map((id) => {
            const tab = byId.get(id);
            if (!tab) {
              return null;
            }
            const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
            const moduleTitle = getModule(tab.moduleId)?.title ?? tab.moduleId;
            return (
              <CommandItem
                key={id}
                value={`${tab.title} ${moduleTitle}`}
                onSelect={() => {
                  setActiveTab(id);
                  onClose();
                }}
              >
                <ModuleIcon className="size-3.5 shrink-0 opacity-70" />
                <span className="truncate">{tab.title}</span>
                {size === "large" ? (
                  <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                    {moduleTitle}
                  </span>
                ) : null}
              </CommandItem>
            );
          })}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
