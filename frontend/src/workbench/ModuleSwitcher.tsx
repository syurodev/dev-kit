import { useEffect, useRef, useState, type ReactNode } from "react";
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
import type { ModuleGroup } from "@/modules/groups";
import { getModule, viewIcon, type ModulePeekProps } from "@/modules/registry";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { MAX_GROUPS } from "@/stores/documentGroups";
import { useModuleGroupsStore } from "@/stores/moduleGroups";
import { useSwitcherPeekStore } from "@/stores/switcherPeek";
import { representative, type SwitcherItem, type SwitcherModule } from "./switcherItems";

export type { SwitcherModule };

type ModuleSwitcherProps = {
  items: SwitcherItem[];
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
export function ModuleSwitcher({ items, activeModuleId, onSelectModule }: ModuleSwitcherProps) {
  return (
    <nav aria-label="Modules" className="flex shrink-0 items-center gap-0.5">
      {items.map((item, index) => {
        // There is no ⌘10. The handler is in Workbench.tsx.
        const digit = index < 9 ? String(index + 1) : null;
        return item.kind === "module" ? (
          <ModuleButton
            key={item.module.id}
            module={item.module}
            slot={index}
            digit={digit}
            active={item.module.id === activeModuleId}
            onSelect={(beside) => onSelectModule(item.module.id, { beside })}
          />
        ) : (
          <GroupButton
            key={item.group.id}
            group={item.group}
            members={item.members}
            slot={index}
            digit={digit}
            activeModuleId={activeModuleId}
            onSelect={(moduleId, beside) => onSelectModule(moduleId, { beside })}
          />
        );
      })}
    </nav>
  );
}

/**
 * Figma `Titlebar / Module Group`: several modules on one button. It shows the
 * member on screen — or, with none, the one used last — and a click opens that
 * one, so what you do every day stays one click away; the corner notch says
 * there are more, and hovering lists them (`Titlebar / Group Peek`).
 *
 * The members are still modules: right-click and ⌥-click act on the one the
 * button stands for, exactly as they would on its own button.
 */
function GroupButton({
  group,
  members,
  slot,
  digit,
  activeModuleId,
  onSelect,
}: {
  group: ModuleGroup;
  members: SwitcherModule[];
  /** Its place in the bar, which is what ⌘⌥digit names. */
  slot: number;
  digit: string | null;
  activeModuleId: string;
  onSelect: (moduleId: string, beside?: boolean) => void;
}) {
  const [peekOpen, setPeekOpen] = useState(false);
  const keyboardOpen = useSwitcherPeekStore((state) => state.slot === slot);
  const closeKeyboardPeek = useSwitcherPeekStore((state) => state.close);
  const lastUsed = useModuleGroupsStore((state) => state.lastUsed[group.id]);
  const shown = representative(members, lastUsed, activeModuleId, group.primary);
  const active = members.some((member) => member.id === activeModuleId);
  const Icon = (shown ? getModule(shown.id)?.icon : undefined) ?? Wrench;

  const label = (
    <>
      {group.title}
      {digit ? <Kbd className="ml-1.5">{shortcut(digit)}</Kbd> : null}
    </>
  );

  const button = (
    <Button
      variant="titlebar"
      size="icon-sm"
      disabled={!shown}
      aria-label={group.title}
      aria-current={active ? "page" : undefined}
      aria-haspopup="dialog"
      aria-keyshortcuts={digit ? `${modKey() === "⌘" ? "Meta" : "Control"}+${digit}` : undefined}
      className={cn("relative", active && "bg-accent text-foreground")}
      onPointerEnter={() => {
        void (shown ? getModule(shown.id)?.views[0]?.preload?.() : undefined)?.catch(() => undefined);
      }}
      onClick={(event) => {
        setPeekOpen(false);
        if (shown) onSelect(shown.id, event.altKey);
      }}
    >
      <Icon className="size-4" />
      {/* The notch: a corner of the button folded over, the way a toolbar marks
          a button that hides others. Its colour follows the button's state. */}
      <span
        aria-hidden
        className={cn(
          "absolute right-1 bottom-1 size-1.25 bg-current [clip-path:polygon(100%_0,100%_100%,0_100%)] transition-colors duration-(--motion-fast) ease-(--ease-out-strong)",
          active ? "text-text-primary" : "text-text-tertiary group-hover/button:text-text-secondary",
        )}
      />
    </Button>
  );

  return (
    <HoverCard
      open={peekOpen || keyboardOpen}
      onOpenChange={(open) => {
        setPeekOpen(open);
        if (!open) closeKeyboardPeek();
      }}
      openDelay={250}
      closeDelay={120}
    >
      {/* The peek gives way to the menu: both would open from the one button. */}
      <ContextMenu onOpenChange={(open) => open && setPeekOpen(false)}>
        <HoverCardTrigger asChild>
          <ContextMenuTrigger asChild>{button}</ContextMenuTrigger>
        </HoverCardTrigger>
        {shown ? (
          <ModuleContextMenu
            moduleId={shown.id}
            digit={digit}
            onSelect={(beside) => onSelect(shown.id, beside)}
          />
        ) : null}
      </ContextMenu>
      <HoverCardContent side="bottom" align="center" className="flex w-80 flex-col p-1">
        <PeekKeyboard active={keyboardOpen} onEscape={closeKeyboardPeek}>
          <div className="flex items-center justify-between px-2 py-1 text-caption-11-medium text-text-tertiary">
            {label}
          </div>
          {members.map((member) => (
            <GroupPeekRow
              key={member.id}
              member={member}
              current={member.id === shown?.id}
              onSelect={() => {
                setPeekOpen(false);
                closeKeyboardPeek();
                onSelect(member.id);
              }}
            />
          ))}
          {shown ? (
            <p className="mt-0.5 border-t border-border-subtle px-2 pt-1.5 pb-1 text-caption-11 text-text-tertiary">
              Click opens {shown.title}, the one you used last.
            </p>
          ) : null}
        </PeekKeyboard>
      </HoverCardContent>
    </HoverCard>
  );
}

/**
 * What makes a peek usable without the pointer. When ⌘⌥digit opened it, focus
 * moves to its current row (else its first), ↑ ↓ Home End move between the
 * rows, Enter or Space picks — they are buttons — and Esc closes it and puts
 * focus back where it was. A peek opened by hovering is left as it always was:
 * nothing here runs, and the rows are still reached with the mouse.
 *
 * Generic over the peek's content on purpose: a module's own peek (Agent Hub
 * lists its sessions) is any set of buttons, and gets the same keys.
 */
function PeekKeyboard({
  active,
  onEscape,
  children,
}: {
  active: boolean;
  onEscape: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const restore = useRef<HTMLElement | null>(null);
  const escaped = useRef(false);

  useEffect(() => {
    if (!active) return;
    restore.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    escaped.current = false;
    // After the card has been placed: the content mounts a frame after the request.
    const frame = requestAnimationFrame(() => {
      const root = ref.current;
      (root?.querySelector<HTMLElement>("button[aria-current]:not(:disabled)") ??
        root?.querySelector<HTMLElement>("button:not(:disabled)"))?.focus();
    });
    return () => {
      cancelAnimationFrame(frame);
      // Only Esc puts focus back; picking a row moves on to somewhere else.
      if (escaped.current) restore.current?.focus();
    };
  }, [active]);

  return (
    <div
      ref={ref}
      className="contents"
      onKeyDown={(event) => {
        if (!active) return;
        if (event.key === "Escape") {
          escaped.current = true;
          onEscape();
          return;
        }
        const keys = ["ArrowDown", "ArrowUp", "Home", "End"];
        if (!keys.includes(event.key)) return;
        const rows = [...(ref.current?.querySelectorAll<HTMLElement>("button:not(:disabled)") ?? [])];
        if (rows.length === 0) return;
        event.preventDefault();
        const at = rows.indexOf(document.activeElement as HTMLElement);
        const next =
          event.key === "Home"
            ? 0
            : event.key === "End"
              ? rows.length - 1
              : event.key === "ArrowDown"
                ? (at + 1) % rows.length
                : (at - 1 + rows.length) % rows.length;
        rows[next].focus();
      }}
    >
      {children}
    </div>
  );
}

/** Figma `Titlebar / Group Peek Row`: a member, what it has open, and a dot on the one the button stands for. */
function GroupPeekRow({
  member,
  current,
  onSelect,
}: {
  member: SwitcherModule;
  current: boolean;
  onSelect: () => void;
}) {
  const Icon = getModule(member.id)?.icon ?? Wrench;
  const open = useWorkbenchTabsStore((state) => moduleTabs(state.modules, member.id).openTabs.length);
  return (
    <button
      type="button"
      disabled={member.unavailable}
      aria-current={current ? "true" : undefined}
      className={cn(MENU_ROW, "min-w-0 text-left", current && "bg-state-soft")}
      onClick={onSelect}
    >
      <MenuRow
        icon={<Icon />}
        trailing={
          <span className="flex items-center gap-2">
            <span className="text-body-12 tabular-nums text-text-tertiary">
              {open === 0 ? "Nothing open" : `${open} open`}
            </span>
            {/* Always drawn, so the counts line up whether or not this row has it. */}
            <span className={cn("size-1.5 rounded-full", current ? "bg-action-primary" : "bg-transparent")} />
          </span>
        }
      >
        {member.title}
      </MenuRow>
    </button>
  );
}

function ModuleButton({
  module,
  slot,
  digit,
  active,
  onSelect,
}: {
  module: SwitcherModule;
  /** Its place in the bar, which is what ⌘⌥digit names. */
  slot: number;
  digit: string | null;
  active: boolean;
  onSelect: (beside?: boolean) => void;
}) {
  const [peekOpen, setPeekOpen] = useState(false);
  const keyboardOpen = useSwitcherPeekStore((state) => state.slot === slot);
  const closeKeyboardPeek = useSwitcherPeekStore((state) => state.close);
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
    closeKeyboardPeek();
    onSelect();
  }

  return (
    <HoverCard
      open={peekOpen || keyboardOpen}
      onOpenChange={(open) => {
        setPeekOpen(open);
        if (!open) closeKeyboardPeek();
      }}
      openDelay={250}
      closeDelay={120}
    >
      {/* The peek gives way to the menu: both would open from the one button. */}
      <ContextMenu onOpenChange={(open) => open && setPeekOpen(false)}>
        <HoverCardTrigger asChild>
          <ContextMenuTrigger asChild>{button}</ContextMenuTrigger>
        </HoverCardTrigger>
        {menu}
      </ContextMenu>
      <HoverCardContent side="bottom" align="center" className="flex w-80 flex-col p-1">
        <PeekKeyboard active={keyboardOpen} onEscape={closeKeyboardPeek}>
          <div className="flex items-center justify-between px-2 py-1 text-caption-11-medium text-text-tertiary">
            {label}
          </div>
          {Peek ? (
            <Peek focusModule={focusFromPeek} />
          ) : (
            <ModuleTabsPeek moduleId={module.id} focusModule={focusFromPeek} />
          )}
        </PeekKeyboard>
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
