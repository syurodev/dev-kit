import { ArrowLeftRight, Wrench } from "lucide-react";
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
  onClose: () => void;
};

/**
 * The detail popup reached by holding ⌘⇧E (or pressing E again) once
 * already browsing the real tab strip — see Workbench.tsx's keydown/keyup
 * handling. A first, un-held ⌘⇧E press never opens this: it hands control
 * to TabStrip.tsx's own ArrowLeft/ArrowRight + Enter navigation directly
 * on the real tabs instead, no popup at all. MRU order, not the tab
 * strip's own left-to-right/module-clustered order, since the whole point
 * of escalating here is jumping back to whatever was just being looked at
 * with more detail than the strip itself shows.
 *
 * No CommandInput (unlike CommandPalette): releasing either modifier key
 * closes this popup (see Workbench.tsx), which means there's never a
 * window where the user could actually type a search term without also
 * closing it — showing a search box that can't practically be used would
 * be misleading. Arrow keys + Enter over CommandList/CommandItem work
 * without an Input present; cmdk focuses the first item automatically.
 */
export function TabSwitcher({ open, onClose }: TabSwitcherProps) {
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const tabMru = useWorkbenchTabsStore((state) => state.tabMru);
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
      <div className="flex items-center gap-1.5 border-b border-border/50 px-3 py-2 text-xs font-medium text-muted-foreground">
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
                <span className="ml-auto shrink-0 text-xs text-muted-foreground">
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
