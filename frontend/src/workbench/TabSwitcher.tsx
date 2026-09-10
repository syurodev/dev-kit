import { ArrowLeftRight, Wrench } from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { getModule } from "../modules/registry";
import { selectActiveModuleTabs, useWorkbenchTabsStore } from "../stores/workbenchTabs";

type TabSwitcherProps = {
  open: boolean;
  onClose: () => void;
};

/**
 * The detail popup reached by holding ⌘⇧E, or pressing E again while already
 * browsing the strip (Workbench.tsx's keydown/keyup). A first, un-held press
 * never opens it — that hands control to TabStrip's own arrow navigation on the
 * real tabs. MRU order rather than the strip's module-clustered order: the point
 * of escalating here is jumping back to what was just being looked at.
 *
 * No CommandInput, unlike CommandPalette: releasing either modifier closes the
 * popup, so there is no window in which a search term could be typed, and a
 * search box that cannot be used is misleading. Arrows and Enter work over
 * CommandList without an Input, and cmdk focuses the first item.
 */
export function TabSwitcher({ open, onClose }: TabSwitcherProps) {
  // One module's tabs, not everyone's: tabs belong to the module that opened
  // them, and switching between modules is the Dock's job (module-tabs.mdx
  // names cross-module navigation a non-goal of per-module ownership).
  const { openTabs, activeTabId, tabMru } = useWorkbenchTabsStore(selectActiveModuleTabs);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);

  // A module's home view opened via the Dock (no specific item picked yet)
  // has no strip entry either — see TabStrip.tsx's isHiddenHomePreview —
  // so it shouldn't show up here as something to "switch to" either.
  const switchableTabs = openTabs.filter(
    (tab) => !(tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId),
  );
  const byId = new Map(switchableTabs.map((tab) => [tab.id, tab]));
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
      className="sm:max-w-2xl"
    >
      {/* CommandDialog's own title/description render sr-only (see
          components/ui/command.tsx), so without a visible header this
          popup was indistinguishable at a glance from the ⌘K Commands
          palette — both are the same CommandDialog chrome around a plain
          list. This row is the actual visual differentiator. */}
      <div className="flex items-center gap-1.5 border-b border-border/50 px-3 py-2 text-caption-11-medium text-text-tertiary">
        <ArrowLeftRight className="size-3.5" />
        Switch tab
      </div>
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
                <span className="ml-auto shrink-0 text-caption-11 text-text-tertiary">
                  {moduleTitle}
                </span>
              </CommandItem>
            );
          })}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
