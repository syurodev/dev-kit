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
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import { runCommand } from "@/modules/registry";

type CommandPaletteProps = {
  open: boolean;
  commands: Array<{ id: string; title: string; shortcut?: string }>;
  onClose: () => void;
};

export function CommandPalette({ open, commands, onClose }: CommandPaletteProps) {
  return (
    <CommandDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
    >
      <CommandInput placeholder="Search commands..." />
      <CommandList>
        <CommandEmpty>No commands are currently available.</CommandEmpty>
        <CommandGroup heading="COMMANDS">
          {commands.map((command) => (
            <CommandItem
              key={command.id}
              value={command.title}
              // It ran nothing at all before: every item closed the palette and
              // that was the whole of it, so the palette listed the app's
              // commands without being able to perform one.
              onSelect={() => {
                runCommand(command.id);
                onClose();
              }}
            >
              {command.title}
              {command.shortcut ? (
                <CommandShortcut>{command.shortcut}</CommandShortcut>
              ) : null}
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
      <CommandFooter />
    </CommandDialog>
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
