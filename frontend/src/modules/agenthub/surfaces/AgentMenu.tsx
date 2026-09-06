import type { ReactNode } from "react";
import { Command as CommandIcon, FileText, Folder, FolderOpen, Bot } from "lucide-react";

import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { AvailableCommand } from "../types";

export type AgentMenuMode = "commands" | "context";

export type ContextFileItem = {
  path: string;
  name?: string;
  description?: string;
  /** Chooses the glyph. Figma draws a file, a folder and an agent mark. */
  kind?: "file" | "folder" | "agent";
};

export type AgentMenuProps = {
  mode: AgentMenuMode;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  onSelectCommand?: (commandName: string) => void;
  onSelectContext?: (filePath: string) => void;
  /** Opens the native file picker. Rendered first, above whatever is listed. */
  onBrowseContext?: () => void;
  /** The control that opens the menu — the composer passes its pill. */
  children: ReactNode;
  className?: string;
};

/**
 * Agent / Menu — Figma `1044:3178`, modes `1044:3124` Commands and
 * `1044:3147` Context.
 *
 * Both modes are one layout: a quiet header label, then rows of
 * [glyph 12] [name, Code/12, primary] [hint, Caption/11, tertiary, right].
 * Only the header text, the glyphs and the data change, so this is one list
 * with two sources rather than two menus.
 *
 * Built on DropdownMenu rather than a positioned div. The version this replaces
 * was a hand-rolled card with its own search box that Figma does not have — and
 * it had no caller at all, so none of it had ever run. Radix brings the
 * positioning, the keyboard walk, the focus trap and the outside-click that a
 * menu needs and that the hand-rolled one had none of.
 *
 * Surface note: the menu sits on `surface/glass-floating` (72%), not on the
 * `glass-float` (62%) that DropdownMenuContent defaults to. Two different float
 * tiers exist and this is the denser one, so the tier is overridden here.
 */

const CONTEXT_GLYPH = {
  file: FileText,
  folder: Folder,
  agent: Bot,
} as const;

export function AgentMenu({
  mode,
  commands = [],
  contextFiles = [],
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  children,
  className,
}: AgentMenuProps) {
  const isCommands = mode === "commands";

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        data-testid="agent-menu"
        data-mode={mode}
        className={cn(
          "w-[320px] rounded-control border-border-subtle bg-surface-glass-floating p-1",
          className,
        )}
      >
        <DropdownMenuLabel className="p-2 text-caption-11 text-text-tertiary">
          {isCommands ? "Commands" : "Add context"}
        </DropdownMenuLabel>

        {!isCommands && onBrowseContext ? (
          // First, and present even when the list below is empty: without a
          // way in, an empty context menu is a dead end rather than a menu.
          <DropdownMenuItem
            data-testid="agent-menu-browse"
            onSelect={onBrowseContext}
            className="h-8 gap-2 rounded-[6px] p-2"
          >
            <FolderOpen className="size-3 shrink-0" aria-hidden="true" />
            <span className="min-w-0 flex-1 truncate text-body-12 text-text-primary">
              Browse files…
            </span>
          </DropdownMenuItem>
        ) : null}

        {isCommands
          ? commands.map((command) => (
              <DropdownMenuItem
                key={command.name}
                data-testid="agent-menu-command"
                onSelect={() => onSelectCommand?.(command.name)}
                className="h-8 gap-2 rounded-[6px] p-2"
              >
                <CommandIcon className="size-3 shrink-0" aria-hidden="true" />
                <span className="min-w-0 flex-1 truncate text-code-12 text-text-primary">
                  {command.name.startsWith("/") ? command.name : `/${command.name}`}
                </span>
                {command.description ? (
                  <span className="shrink-0 truncate text-caption-11 text-text-tertiary">
                    {command.description}
                  </span>
                ) : null}
              </DropdownMenuItem>
            ))
          : contextFiles.map((file) => {
              const Glyph = CONTEXT_GLYPH[file.kind ?? "file"];
              return (
                <DropdownMenuItem
                  key={file.path}
                  data-testid="agent-menu-context"
                  onSelect={() => onSelectContext?.(file.path)}
                  className="h-8 gap-2 rounded-[6px] p-2"
                >
                  <Glyph className="size-3 shrink-0" aria-hidden="true" />
                  <span className="min-w-0 flex-1 truncate text-code-12 text-text-primary">
                    {file.name ?? file.path}
                  </span>
                  {file.description ? (
                    <span className="shrink-0 truncate text-caption-11 text-text-tertiary">
                      {file.description}
                    </span>
                  ) : null}
                </DropdownMenuItem>
              );
            })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
