import { useState } from "react";
import { Wrench } from "lucide-react";
import { Button } from "@/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Kbd } from "@/components/ui/kbd";
import { MENU_ROW, MenuRow } from "@/components/ui/menu-row";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { modKey, shortcut } from "@/lib/shortcut";
import { cn } from "@/lib/utils";
import { getModule, viewIcon, type ModulePeekProps } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";

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

  if (!peeks) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
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
      <HoverCardTrigger asChild>{button}</HoverCardTrigger>
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
