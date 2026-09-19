import { useEffect, useMemo, useRef, useState } from "react";
import {
  CommandDialog,
  CommandEmpty,
  CommandFooter,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { ChevronRight, Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { getModule, type ModuleManifest, type PaletteSearchGroup } from "@/modules/registry";
import { allOpenTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { shortcut } from "@/lib/shortcut";
import {
  composeSections,
  palettePlaceholder,
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
  unavailableModuleIds: ReadonlySet<string>;
  activeModuleId: string | null;
  /** The shell's own: it also preloads the module's home view. */
  onFocusModule: (moduleId: string) => void;
};

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

  useEffect(() => {
    if (!open || !search) {
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
          .search(query.trim(), controller.signal)
          // A module's search failing is that module's problem, and must not
          // cost the commands and modules underneath it.
          .catch(() => [] as PaletteSearchGroup[])
          .then((groups) => {
            if (controller.signal.aborted) return;
            setFound(groups);
            setSearching(false);
          });
      },
      query ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [open, search, query]);

  const paletteModules = useMemo<PaletteModule[]>(
    () =>
      modules.map((module) => {
        const digit = switcherModuleIds.indexOf(module.id);
        return {
          id: module.id,
          title: module.title,
          icon: module.icon,
          switchable: digit !== -1,
          shortcut: digit !== -1 && digit < 9 ? shortcut(String(digit + 1)) : undefined,
          unavailable: unavailableModuleIds.has(module.id),
          commands: module.commands,
        };
      }),
    [modules, switcherModuleIds, unavailableModuleIds],
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
      { query, activeModuleId, modules: paletteModules, tabs },
      { focusModule: onFocusModule, activateTab: (id) => useWorkbenchTabsStore.getState().setActiveTab(id) },
    );
    return composeSections(query, activeModuleId, fixed, found);
  }, [query, activeModuleId, paletteModules, tabModules, found, onFocusModule]);

  // The list keeps its scroll between queries otherwise, so new results could
  // open scrolled past their own first row.
  useEffect(() => {
    listRef.current?.scrollTo({ top: 0 });
  }, [query]);

  const activeModule = paletteModules.find((module) => module.id === activeModuleId);
  const empty = sections.length === 0;

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
          and at the primitive's 20rem the second group starts below the fold. */}
      <CommandList ref={listRef} className="max-h-[min(56vh,28rem)]">
        {empty && !searching ? (
          <CommandEmpty title="Nothing found">
            {search ? `No ${search.noun}, command, module or open tab matches.` : "No command, module or open tab matches."}
          </CommandEmpty>
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
        {activeModule ? (
          <>
            <activeModule.icon className="size-3.5 shrink-0" aria-hidden />
            <span className="truncate">{searching ? `Searching ${activeModule.title}…` : activeModule.title}</span>
          </>
        ) : null}
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
        "flex w-[640px] max-w-[640px] flex-col overflow-hidden rounded-float bg-glass-float backdrop-blur-[20px] border border-glass-border shadow-none text-text-primary select-none",
        className
      )}
    >
      {/* Search row: h56, pl-5 pr-4, gap-3 */}
      <div className="flex h-14 shrink-0 items-center gap-3 border-b border-border-subtle pl-5 pr-4">
        <Search className="size-5 shrink-0 text-text-tertiary" />
        <span className="flex-1 text-title-16 text-text-primary">
          {state === "results" ? "git status" : "nonexistent-command"}
        </span>
        <kbd className="rounded-sm border border-border-subtle bg-surface-raised p-1 text-code-11 text-text-tertiary">
          esc
        </kbd>
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
            <span className="ml-auto rounded-sm border border-border-subtle bg-surface-raised p-1 text-code-11 text-text-tertiary">⌘⇧G</span>
          </div>
          {/* Inactive row: NO background at all + label 13 Regular --text-secondary */}
          <div
            data-active="false"
            className="flex h-10 items-center gap-3 rounded-md bg-transparent px-3 text-body-13 text-text-secondary cursor-pointer"
          >
            <span>Git: Commit Changes</span>
            <span className="ml-auto rounded-sm border border-border-subtle bg-surface-raised p-1 text-code-11 text-text-tertiary">⌘K</span>
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
        <span className="whitespace-pre">{`↑↓ navigate   ·   ↵ open   ·   esc close`}</span>
      </div>
    </div>
  );
}
