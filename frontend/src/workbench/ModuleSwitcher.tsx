import { useState } from "react";
import { Columns2, Plus, SquareArrowOutUpRight, Wrench, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Kbd } from "@/components/ui/kbd";
import { MENU_ROW, MenuRow } from "@/components/ui/menu-row";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { modKey, shortcut } from "@/lib/shortcut";
import { cn } from "@/lib/utils";
import { getModule, viewIcon, type ModulePeekProps } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { MAX_GROUPS } from "@/stores/documentGroups";

export type SwitcherModule = { id: string; title: string; unavailable?: boolean };

type ModuleSwitcherProps = {
  modules: SwitcherModule[];
  activeModuleId: string;
  /** `beside`: ⌥-click, open the module in a new group beside the focused one. */
  onSelectModule: (moduleId: string, opts?: { beside?: boolean }) => void;
};

/**
 * The module switcher, in the title bar. It replaced the floating dock, which
 * said which module you were in only while the pointer was at the window's
 * bottom edge, and covered the bottom slot while it did.
 *
 * Each module owns what its button carries: its icon, a `badge`, and a `peek`
 * that opens on hover. See ModuleManifest.
 */
export function ModuleSwitcher({ modules, activeModuleId, onSelectModule }: ModuleSwitcherProps) {
  return (
    <nav aria-label="Modules" className="flex shrink-0 items-center gap-0.5">
      {modules.map((module, index) => (
        <ModuleButton
          key={module.id}
          module={module}
          // There is no ⌘10. The handler is in Workbench.tsx.
          digit={index < 9 ? String(index + 1) : null}
          active={module.id === activeModuleId}
          onSelect={(beside) => onSelectModule(module.id, { beside })}
        />
      ))}
    </nav>
  );
}

function ModuleButton({
  module,
  digit,
  active,
  onSelect,
}: {
  module: SwitcherModule;
  digit: string | null;
  active: boolean;
  onSelect: (beside?: boolean) => void;
}) {
  const [peekOpen, setPeekOpen] = useState(false);
  const manifest = getModule(module.id);
  const Icon = manifest?.icon ?? Wrench;
  const Badge = manifest?.badge;
  // A module's own peek, or its open tabs when it has a strip.
  const Peek = manifest?.peek;
  const peeks = Boolean(Peek || manifest?.tabs);

  const label = (
    <>
      {module.title}
      {digit ? <Kbd className="ml-1.5">{shortcut(digit)}</Kbd> : null}
    </>
  );

  const button = (
    <Button
      variant="titlebar"
      size="icon-sm"
      disabled={module.unavailable}
      aria-label={module.title}
      aria-current={active ? "page" : undefined}
      aria-keyshortcuts={
        digit ? `${modKey() === "⌘" ? "Meta" : "Control"}+${digit}` : undefined
      }
      className={cn("relative", active && "bg-accent text-foreground")}
      onPointerEnter={() => {
        // A click still renders through Suspense and owns any load error; a
        // speculative hover must never leave an unhandled rejection.
        void manifest?.views[0]?.preload?.().catch(() => undefined);
      }}
      onClick={(event) => {
        setPeekOpen(false);
        onSelect(event.altKey);
      }}
    >
      <Icon className="size-4" />
      {Badge ? <Badge /> : null}
    </Button>
  );

  const menu = (
    <ModuleContextMenu moduleId={module.id} digit={digit} unavailable={module.unavailable} onSelect={onSelect} />
  );

  // Roots first, triggers after, as on a tab: a Radix Root is a provider with
  // no element, so the two triggers nest directly and both land on the button.
  if (!peeks) {
    return (
      <Tooltip>
        <ContextMenu>
          <TooltipTrigger asChild>
            <ContextMenuTrigger asChild>{button}</ContextMenuTrigger>
          </TooltipTrigger>
          {menu}
        </ContextMenu>
        <TooltipContent side="bottom">{label}</TooltipContent>
      </Tooltip>
    );
  }

  function focusFromPeek() {
    setPeekOpen(false);
    onSelect();
  }

  return (
    <HoverCard open={peekOpen} onOpenChange={setPeekOpen} openDelay={250} closeDelay={120}>
      {/* The peek gives way to the menu: both would open from the one button. */}
      <ContextMenu onOpenChange={(open) => open && setPeekOpen(false)}>
        <HoverCardTrigger asChild>
          <ContextMenuTrigger asChild>{button}</ContextMenuTrigger>
        </HoverCardTrigger>
        {menu}
      </ContextMenu>
      <HoverCardContent side="bottom" align="center" className="flex w-80 flex-col p-1">
        <div className="flex items-center justify-between px-2 py-1 text-caption-11-medium text-text-tertiary">
          {label}
        </div>
        {Peek ? (
          <Peek focusModule={focusFromPeek} />
        ) : (
          <ModuleTabsPeek moduleId={module.id} focusModule={focusFromPeek} />
        )}
      </HoverCardContent>
    </HoverCard>
  );
}

/** ⌥-click on a Mac, Alt+Click elsewhere — the same modifier the click reads. */
const altClickLabel = () => (modKey() === "⌘" ? "⌥ Click" : "Alt+Click");

/**
 * Right-click on a module: where to open it. The same choices as a click and
 * an ⌥-click, said out loud, plus what only makes sense once it is on screen.
 */
function ModuleContextMenu({
  moduleId,
  digit,
  unavailable,
  onSelect,
}: {
  moduleId: string;
  digit: string | null;
  unavailable?: boolean;
  onSelect: (beside?: boolean) => void;
}) {
  const groups = useWorkbenchTabsStore((s) => s.groups);
  const closeGroup = useWorkbenchTabsStore((s) => s.closeGroup);
  const shownIn = groups.find((g) => g.moduleId === moduleId);
  const manifest = getModule(moduleId);
  const newTab = manifest?.newTab;

  return (
    <ContextMenuContent className="w-60">
      <ContextMenuItem
        disabled={unavailable}
        icon={<SquareArrowOutUpRight />}
        shortcut={digit ? shortcut(digit) : undefined}
        onSelect={() => onSelect(false)}
      >
        {shownIn ? "Go to Group" : "Open"}
      </ContextMenuItem>
      <ContextMenuItem
        // Already on screen: beside would only focus it, which Open does.
        disabled={unavailable || Boolean(shownIn) || groups.length >= MAX_GROUPS}
        icon={<Columns2 />}
        shortcut={altClickLabel()}
        onSelect={() => onSelect(true)}
      >
        Open Beside
      </ContextMenuItem>
      {newTab ? (
        <ContextMenuItem
          disabled={unavailable}
          icon={<Plus />}
          onSelect={() => {
            onSelect(false);
            newTab();
          }}
        >
          New Tab
        </ContextMenuItem>
      ) : null}
      {shownIn && groups.length > 1 ? (
        <>
          <ContextMenuSeparator />
          <ContextMenuItem icon={<X />} onSelect={() => closeGroup(shownIn.id)}>
            Close Group
          </ContextMenuItem>
        </>
      ) : null}
    </ContextMenuContent>
  );
}

/** The peek every module with a tab strip gets: its open tabs, in strip order. */
function ModuleTabsPeek({ moduleId, focusModule }: ModulePeekProps & { moduleId: string }) {
  const { openTabs, activeTabId } = useWorkbenchTabsStore((state) =>
    moduleTabs(state.modules, moduleId),
  );
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);

  if (openTabs.length === 0) {
    return <p className="px-2 pb-1.5 text-body-12 text-text-tertiary">Nothing open</p>;
  }
  return (
    <div className="flex max-h-80 flex-col overflow-y-auto">
      {openTabs.map((tab) => {
        const TabIcon = viewIcon(tab.moduleId, tab.viewId);
        const current = tab.id === activeTabId;
        return (
          <button
            key={tab.id}
            type="button"
            aria-current={current ? "true" : undefined}
            className={cn(MENU_ROW, "min-w-0 text-left", tab.preview && "italic")}
            onClick={() => {
              focusModule();
              setActiveTab(tab.id);
            }}
          >
            <MenuRow
              icon={TabIcon ? <TabIcon /> : null}
              trailing={
                current ? <span className="size-1.5 rounded-full bg-action-primary" /> : null
              }
            >
              {tab.title}
            </MenuRow>
          </button>
        );
      })}
    </div>
  );
}
