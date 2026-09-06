import { useState } from "react";
import { Command as CommandIcon, FileText, Search } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AvailableCommand } from "../types";

export type AgentMenuMode = "commands" | "context";

export type ContextFileItem = {
  path: string;
  name?: string;
  description?: string;
};

export type AgentMenuProps = {
  mode?: AgentMenuMode;
  onModeChange?: (mode: AgentMenuMode) => void;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  onSelectCommand: (commandName: string) => void;
  onSelectContext: (filePath: string) => void;
  onClose?: () => void;
  className?: string;
};

/**
 * Agent / Menu (Figma 320×168)
 * Popover menu for composer offering ACP slash commands and context attachment.
 */
export function AgentMenu({
  mode: controlledMode,
  onModeChange,
  commands = [],
  contextFiles = [],
  onSelectCommand,
  onSelectContext,
  onClose,
  className,
}: AgentMenuProps) {
  const [internalMode, setInternalMode] = useState<AgentMenuMode>("commands");
  const [query, setQuery] = useState("");

  const activeMode = controlledMode ?? internalMode;

  function switchMode(newMode: AgentMenuMode) {
    if (onModeChange) {
      onModeChange(newMode);
    } else {
      setInternalMode(newMode);
    }
  }

  const filteredCommands = commands.filter(
    (cmd) =>
      cmd.name.toLowerCase().includes(query.toLowerCase()) ||
      (cmd.description && cmd.description.toLowerCase().includes(query.toLowerCase())),
  );

  const filteredContextFiles = contextFiles.filter(
    (f) =>
      f.path.toLowerCase().includes(query.toLowerCase()) ||
      (f.name && f.name.toLowerCase().includes(query.toLowerCase())),
  );

  return (
    <div
      role="menu"
      aria-label="Agent Menu"
      data-testid="agent-menu"
      className={cn(
        "flex w-80 max-h-[168px] flex-col rounded-lg border border-border-subtle bg-surface-raised shadow-lg overflow-hidden select-none",
        className,
      )}
    >
      {/* Header tab switcher */}
      <div className="flex h-7 shrink-0 items-center justify-between border-b border-border-subtle bg-surface-base px-2">
        <div className="flex items-center gap-1">
          <button
            type="button"
            data-testid="tab-commands"
            onClick={() => switchMode("commands")}
            className={cn(
              "flex h-5 items-center gap-1 rounded-sm px-1.5 text-caption-11 transition-colors cursor-pointer",
              activeMode === "commands"
                ? "bg-surface-tinted text-text-primary"
                : "text-text-tertiary hover:text-text-secondary",
            )}
          >
            <CommandIcon className="size-3 shrink-0" aria-hidden="true" />
            <span>Commands</span>
          </button>
          <button
            type="button"
            data-testid="tab-context"
            onClick={() => switchMode("context")}
            className={cn(
              "flex h-5 items-center gap-1 rounded-sm px-1.5 text-caption-11 transition-colors cursor-pointer",
              activeMode === "context"
                ? "bg-surface-tinted text-text-primary"
                : "text-text-tertiary hover:text-text-secondary",
            )}
          >
            <FileText className="size-3 shrink-0" aria-hidden="true" />
            <span>Context</span>
          </button>
        </div>

        {/* Filter input */}
        <div className="flex items-center gap-1">
          <Search className="size-3 text-text-tertiary shrink-0" aria-hidden="true" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter..."
            aria-label="Filter menu items"
            className="h-4 w-20 bg-transparent text-caption-11 text-text-primary placeholder:text-text-tertiary outline-none"
          />
        </div>
      </div>

      {/* List content area */}
      <div className="flex-1 overflow-y-auto p-1">
        {activeMode === "commands" ? (
          filteredCommands.length === 0 ? (
            <div className="flex h-20 items-center justify-center text-caption-11 text-text-tertiary">
              No commands available
            </div>
          ) : (
            <div className="flex flex-col gap-0.5">
              {filteredCommands.map((cmd) => (
                <button
                  key={cmd.name}
                  type="button"
                  data-testid={`menu-command-${cmd.name.replace(/^\//, "")}`}
                  onClick={() => {
                    onSelectCommand(cmd.name);
                    onClose?.();
                  }}
                  className="flex h-7 w-full items-center justify-between rounded-sm px-2 text-left transition-colors hover:bg-surface-tinted cursor-pointer"
                >
                  <span className="text-code-12 text-text-primary">{cmd.name}</span>
                  {cmd.description ? (
                    <span className="truncate max-w-[140px] text-caption-11 text-text-secondary">
                      {cmd.description}
                    </span>
                  ) : null}
                </button>
              ))}
            </div>
          )
        ) : filteredContextFiles.length === 0 ? (
          <div className="flex h-20 items-center justify-center text-caption-11 text-text-tertiary">
            No context files
          </div>
        ) : (
          <div className="flex flex-col gap-0.5">
            {filteredContextFiles.map((file) => (
              <button
                key={file.path}
                type="button"
                data-testid={`menu-context-${file.name ?? file.path}`}
                onClick={() => {
                  onSelectContext(file.path);
                  onClose?.();
                }}
                className="flex h-7 w-full items-center justify-between rounded-sm px-2 text-left transition-colors hover:bg-surface-tinted cursor-pointer"
              >
                <div className="flex items-center gap-1.5 truncate">
                  <FileText className="size-3 text-text-tertiary shrink-0" aria-hidden="true" />
                  <span className="text-code-12 text-text-primary truncate">
                    {file.name ?? file.path}
                  </span>
                </div>
                {file.description ? (
                  <span className="text-caption-11 text-text-secondary truncate max-w-[100px]">
                    {file.description}
                  </span>
                ) : null}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
