import { useEffect, useMemo, useRef, useState } from "react";
import {
  CommandDialog,
  CommandEmpty,
  CommandFooter,
  CommandGroup,
  CommandHint,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { ChevronRight, Search } from "lucide-react";
import { Kbd } from "@/components/ui/kbd";
import { cn } from "@/lib/utils";
import { getModule, type ModuleManifest, type PaletteSearchGroup } from "@/modules/registry";
import { allOpenTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { shortcut } from "@/lib/shortcut";
import {
  composeSections,
  paletteEmptyMessage,
  palettePlaceholder,
  parseQuery,
  staticSections,
  type PaletteModule,
  type PaletteRow,
} from "./palette/paletteModel";

type CommandPaletteProps = {
  open: boolean;
  onClose: () => void;
  /** Every module that can be used, in registry order. */
  modules: readonly ModuleManifest[];
  /** The rail's modules in its order — the order ⌘1–⌘9 count in. */
  switcherModuleIds: readonly string[];
  /** Every module on the title bar, a group's members included, with its group's title. */
  barModules: ReadonlyMap<string, string | undefined>;
  unavailableModuleIds: ReadonlySet<string>;
  activeModuleId: string | null;
  /** The shell's own: it also preloads the module's home view. */
  onFocusModule: (moduleId: string) => void;
};

function barRank(barModules: ReadonlyMap<string, string | undefined>, moduleId: string): number {
  const index = [...barModules.keys()].indexOf(moduleId);
  return index === -1 ? Number.MAX_SAFE_INTEGER : index;
}

// Long enough that typing a word is one search, short enough not to be felt.
const SEARCH_DEBOUNCE_MS = 140;

/**
 * The app's one place to go anywhere from: run a command, switch module,
 * reopen a tab, and find something inside the module in front — a table, a
 * file, a note (each module says what through `paletteSearch`).
 *
 * Filtering is ours, not cmdk's. cmdk scores every row against one flat list;
 * the palette has an order that is part of its meaning — what was searched for
 * in this module, then this module's commands, then the rest — and rows that
 * arrive late from a module's search, which cmdk would re-filter with a
 * different idea of a match than the module used. cmdk still owns what it is
 * good at: the keyboard, the selection, the listbox semantics.
 */
export function CommandPalette({
  open,
  onClose,
  modules,
  switcherModuleIds,
  barModules,
  unavailableModuleIds,
  activeModuleId,
  onFocusModule,
}: CommandPaletteProps) {
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<PaletteSearchGroup[]>([]);
  const [searching, setSearching] = useState(false);
  const tabModules = useWorkbenchTabsStore((state) => state.modules);
  const listRef = useRef<HTMLDivElement | null>(null);

  // A palette that opens on the last thing typed makes the second use slower
  // than the first: the old query has to be cleared before the new one.
  useEffect(() => {
    if (!open) {
      setQuery("");
      setFound([]);
      setSearching(false);
    }
  }, [open]);

  const search = activeModuleId ? getModule(activeModuleId)?.paletteSearch : undefined;
  // ">" and "@" narrow what is asked; `text` is what is matched.
  const { scope, text } = useMemo(() => parseQuery(query), [query]);

  useEffect(() => {
    // ">" is commands only: the module is not asked at all.
    if (!open || !search || scope === "commands") {
      setFound([]);
      setSearching(false);
      return;
    }
    const controller = new AbortController();
    setSearching(true);
    // The first ask, on opening, is not debounced: nothing is being typed yet.
    const timer = setTimeout(
      () => {
        search
          .search(text, controller.signal)
          // A module's search failing is that module's problem, and must not
          // cost the commands and modules underneath it.
          .catch(() => [] as PaletteSearchGroup[])
          .then((groups) => {
            if (controller.signal.aborted) return;
            setFound(groups);
            setSearching(false);
          });
      },
      text ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
    // On text and scope, not the raw query: typing the "@" itself, or a space
    // after it, is not a new question for the module.
  }, [open, search, scope, text]);

  const paletteModules = useMemo<PaletteModule[]>(
    () =>
      // In the title bar's order, a group's members where the group stands —
      // the order the person sees — rather than the registry's, which put
      // Accounts first. Anything not on the bar keeps its place after them.
      [...modules]
        .sort((a, b) => barRank(barModules, a.id) - barRank(barModules, b.id))
        .map((module) => {
          const digit = switcherModuleIds.indexOf(module.id);
          return {
            id: module.id,
            title: module.title,
            icon: module.icon,
            switchable: barModules.has(module.id),
            group: barModules.get(module.id),
            shortcut: digit !== -1 && digit < 9 ? shortcut(String(digit + 1)) : undefined,
            unavailable: unavailableModuleIds.has(module.id),
            commands: module.commands,
          };
        }),
    [modules, switcherModuleIds, barModules, unavailableModuleIds],
  );

  const sections = useMemo(() => {
    const state = useWorkbenchTabsStore.getState();
    const frontTabId = activeModuleId ? state.modules[activeModuleId]?.activeTabId : undefined;
    const tabs = allOpenTabs(tabModules).filter(
      (tab) =>
        tab.id !== frontTabId &&
        // A module's home view rides along as a hidden preview tab; it is
        // "the module", which Go to already offers.
        !(tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId),
    );
    const fixed = staticSections(
      { query: text, scope, activeModuleId, modules: paletteModules, tabs },
      { focusModule: onFocusModule, activateTab: (id) => useWorkbenchTabsStore.getState().setActiveTab(id) },
    );
    return composeSections(text, activeModuleId, fixed, scope === "commands" ? [] : found);
  }, [text, scope, activeModuleId, paletteModules, tabModules, found, onFocusModule]);

  // The list keeps its scroll between queries otherwise, so new results could
  // open scrolled past their own first row.
  useEffect(() => {
    listRef.current?.scrollTo({ top: 0 });
  }, [query]);

  const activeModule = paletteModules.find((module) => module.id === activeModuleId);
  const empty = sections.length === 0;
  const emptyMessage = paletteEmptyMessage(scope, activeModule?.title, search?.noun);
  // What the list is scoped to, for the footer: the prefixes are invisible
  // until someone knows them, so the unscoped footer is where they are taught.
  const scopeLabel =
    scope === "commands"
      ? "Commands"
      : scope === "module"
        ? `Only ${activeModule?.title ?? "this module"}`
        : null;

  function pick(row: PaletteRow) {
    // Closed first: a command that opens a dialog of its own must not find
    // this one still holding the focus trap.
    onClose();
    row.run();
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      description="Run a command, switch module, or search the module in front."
      commandProps={{ shouldFilter: false, loop: true }}
    >
      <CommandInput value={query} onValueChange={setQuery} placeholder={palettePlaceholder(search?.noun)} />
      {/* Taller than a picker's list: this one holds several groups at once,
          and at the primitive's 20rem the second group starts below the fold.
          No vh here — fitting the window is the dialog's job (command.tsx caps
          the whole column), and a second guess at it only made the list
          shorter than the room it had. */}
      <CommandList ref={listRef} className="max-h-112">
        {empty && !searching ? (
          <CommandEmpty title={emptyMessage.title}>{emptyMessage.body}</CommandEmpty>
        ) : null}
        {empty && searching ? (
          <p className="px-3 pt-10 pb-12 text-center text-body-13 text-text-tertiary">Searching {search?.noun}…</p>
        ) : null}
        {sections.map((section) => (
          <CommandGroup key={section.id} heading={section.heading}>
            {section.rows.map((row) => (
              <PaletteItem key={row.id} row={row} onPick={pick} />
            ))}
          </CommandGroup>
        ))}
      </CommandList>
      <CommandFooter>
        {scopeLabel ? (
          <span className="truncate text-text-secondary">{searching ? `Searching ${activeModule?.title}…` : scopeLabel}</span>
        ) : searching && activeModule ? (
          <span className="truncate">Searching {activeModule.title}…</span>
        ) : (
          // The module's name rather than its noun: "endpoints, files and
          // symbols" beside the key hints does not fit in 640px, and the name
          // is what "@" is scoped to anyway. No "@" hint where it finds nothing.
          <>
            <CommandHint keys=">">commands</CommandHint>
            {search && activeModule ? <CommandHint keys="@">{activeModule.title}</CommandHint> : null}
          </>
        )}
      </CommandFooter>
    </CommandDialog>
  );
}

function PaletteItem({ row, onPick }: { row: PaletteRow; onPick: (row: PaletteRow) => void }) {
  // A command has no glyph of its own, and an empty column beside one reads as
  // something missing. The chevron says "this does something" and keeps the
  // titles in line with the rows that do have one.
  const Icon = row.icon ?? ChevronRight;
  return (
    // value is the id, not the title: two rows may share a title (a table in
    // two schemas), and cmdk tracks its selection by value.
    <CommandItem value={row.id} indicator={false} onSelect={() => onPick(row)}>
      <Icon
        className="size-4 shrink-0 text-text-tertiary group-data-[selected=true]/command-item:text-text-secondary"
        aria-hidden
      />
      <span className="min-w-0 shrink truncate">{row.title}</span>
      {row.subtitle ? (
        <span className="min-w-0 flex-1 truncate text-body-13 font-normal text-text-tertiary">{row.subtitle}</span>
      ) : null}
      {row.shortcut ? <CommandShortcut>{row.shortcut}</CommandShortcut> : null}
    </CommandItem>
  );
}

/**
 * Static presentation of Command Palette (Figma 719:1475 — Results / Empty)
 * for ComponentGallery and DOM measurement without mounting live dialog modals.
 */
export function CommandPaletteStaticCard({
  state = "results",
  id,
  className,
}: {
  state?: "results" | "empty";
  id?: string;
  className?: string;
}) {
  return (
    <div
      id={id}
      data-slot="command-palette-static"
      data-state={state}
      className={cn(
        "flex w-160 max-w-160 flex-col overflow-hidden rounded-float bg-glass-float backdrop-blur-float border border-glass-border shadow-none text-text-primary select-none",
        className
      )}
    >
      {/* Search row: h56, pl-5 pr-4, gap-3 */}
      <div className="flex h-14 shrink-0 items-center gap-3 border-b border-border-subtle pl-5 pr-4">
        <Search className="size-5 shrink-0 text-text-tertiary" />
        <span className="flex-1 text-title-16 text-text-primary">
          {state === "results" ? "git status" : "nonexistent-command"}
        </span>
        <Kbd>esc</Kbd>
      </div>

      {state === "results" ? (
        <div className="px-2 pt-1 pb-2 flex flex-col gap-1">
          <div className="pt-2 pb-1 pl-3 text-code-11 uppercase font-normal text-text-tertiary">
            COMMANDS
          </div>
          {/* Active row: --surface-raised + label 13 Medium --text-primary */}
          <div
            data-active="true"
            className="flex h-10 items-center gap-3 rounded-md bg-surface-raised px-3 text-body-13-medium text-text-primary cursor-pointer"
          >
            <span>Git: Status</span>
            <Kbd className="ml-auto">⌘⇧G</Kbd>
          </div>
          {/* Inactive row: NO background at all + label 13 Regular --text-secondary */}
          <div
            data-active="false"
            className="flex h-10 items-center gap-3 rounded-md bg-transparent px-3 text-body-13 text-text-secondary cursor-pointer"
          >
            <span>Git: Commit Changes</span>
            <Kbd className="ml-auto">⌘K</Kbd>
          </div>
        </div>
      ) : (
        /* Empty list: px-2 gap-2 pt-10 pb-12 items-center */
        <div className="flex flex-col items-center justify-center gap-2 px-2 pt-10 pb-12 text-center">
          <p className="text-title-16 text-text-secondary">No results found</p>
          <p className="text-body-13 text-text-tertiary">
            No commands matched &quot;nonexistent-command&quot;
          </p>
        </div>
      )}

      {/* Footer: h40, px-5 gap-4, Mono 11 --text-tertiary */}
      <div className="flex h-10 shrink-0 items-center gap-4 border-t border-border-subtle px-5 text-code-11 text-text-tertiary">
        <CommandHint keys="↑↓">navigate</CommandHint>
        <CommandHint keys="↵">open</CommandHint>
        <CommandHint keys="esc">close</CommandHint>
      </div>
    </div>
  );
}
