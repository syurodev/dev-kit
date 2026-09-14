import { useSyncExternalStore } from "react";
import { AtSign, Command as CommandIcon, ArrowUp, X } from "lucide-react";
import { BorderBeam } from "border-beam";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { onThemeChange } from "@/theme/themeColors";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";
import { AgentContextMeter } from "./AgentContextMeter";
import type { AvailableCommand, Usage } from "../types";

export type ComposerSize = "wide" | "rail";
export type ComposerState = "idle" | "streaming" | "attached";

export type AgentComposerProps = {
  size?: ComposerSize;
  state?: ComposerState;
  streaming?: boolean;
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  onCancel: () => void;
  usage?: Usage | null;
  turnCount?: number;
  vendor?: string;
  version?: string;
  attachedFiles?: string[];
  onRemoveAttachment?: (path: string) => void;
  onSelectCommand?: (name: string) => void;
  onSelectContext?: (path: string) => void;
  onBrowseContext?: () => void;
  /** Sends /compact from the context popover. Omit when the agent offers no such command. */
  onCompact?: () => void;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  disabled?: boolean;
  placeholder?: string;
  agentName?: string;
  className?: string;
};

/**
 * The app's resolved light/dark mode, read off <html> the way ThemeProvider
 * writes it. Not useTheme(): that throws outside the provider, and the beam's
 * own `theme="auto"` follows the OS, which is not the app's setting.
 */
function useAppTheme(): "dark" | "light" {
  return useSyncExternalStore(
    onThemeChange,
    () => (document.documentElement.dataset.theme === "light" ? "light" : "dark"),
    () => "dark",
  );
}

/**
 * Agent / Composer — Figma `963:2634`, Size Wide/Rail x State Idle/Streaming/
 * Attached.
 *
 * One glass surface that floats over the foot of the transcript. The text
 * scrolls under it and the blur is what keeps the field legible over whatever
 * line is passing — the one place in the module where glass is doing work,
 * because it is the one layer that sits on top of content (liquid-glass.md).
 * With transparency reduced, `.glass-float` falls back to surface/raised.
 *
 * It used to be two boxes: a bordered frame with a meta band — "2 turns ·
 * $0.91", the vendor and version — above an inner field, and
 * "29,480 / 1,000,000 context" spelled out beside the pills. Those numbers are
 * behind the context ring now (AgentContextMeter); the vendor is already in the
 * header one glance up.
 *
 * While a turn runs, a border beam travels the edge. The composer is where the
 * eye is when you have just pressed Enter, and a moving edge there says "it is
 * working" without a line of text; it is warm for Claude, matching its mark in
 * the rail. The library stills itself under Reduce Motion.
 *
 * Size still matters: at 280px the pills lose their labels and the field's top
 * row drops from 56 to 40.
 */
export function AgentComposer({
  size = "wide",
  state,
  streaming = false,
  value,
  onChange,
  onSend,
  onCancel,
  usage,
  turnCount = 0,
  vendor = "claude",
  attachedFiles = [],
  onRemoveAttachment,
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  onCompact,
  commands = [],
  contextFiles = [],
  disabled = false,
  placeholder,
  agentName,
  className,
}: AgentComposerProps) {
  const theme = useAppTheme();
  const rail = size === "rail";
  const isStreaming = streaming || state === "streaming";
  const effectiveState: ComposerState =
    state ?? (isStreaming ? "streaming" : attachedFiles.length > 0 ? "attached" : "idle");

  const inputPlaceholder =
    placeholder ??
    (isStreaming
      ? `${agentName ?? "Agent"} is working…`
      : `Ask ${agentName ?? "agent"}…`);

  return (
    <BorderBeam
      active={isStreaming}
      size="md"
      colorVariant={vendor.toLowerCase().startsWith("claude") ? "sunset" : "ocean"}
      theme={theme}
      strength={0.8}
      className={cn("w-full", rail ? "max-w-70" : "max-w-274", className)}
    >
      <div
        role="region"
        aria-label="Agent prompt composer"
        data-testid="agent-composer"
        data-size={size}
        data-state={effectiveState}
        className="glass-float flex w-full flex-col gap-3 rounded-xl p-3"
      >
        {attachedFiles.length > 0 ? (
          <div data-testid="composer-attachments" className="flex w-full flex-wrap items-center gap-1">
            {attachedFiles.map((path) => (
              <AgentFileChip
                key={path}
                path={path}
                onClick={onRemoveAttachment ? () => onRemoveAttachment(path) : undefined}
              />
            ))}
          </div>
        ) : null}

        <div className={cn("flex w-full items-start gap-3", rail ? "h-10" : "h-14")}>
          {/* A bare textarea, not the shared Textarea: that one brings a border,
              a fill, a focus ring and its own radius, all of which this field
              already provides around it. Overriding six properties to get back
              to plain text is more code than plain text. */}
          <textarea
            rows={1}
            value={value}
            disabled={disabled}
            placeholder={inputPlaceholder}
            aria-label="Agent prompt input"
            data-testid="agent-composer-textarea"
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                if (!isStreaming) onSend();
              }
            }}
            className="h-full min-w-0 flex-1 resize-none bg-transparent text-body-14 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60"
          />
          <Button
            type="button"
            size={rail ? "icon-sm" : "icon"}
            disabled={disabled}
            aria-label={isStreaming ? "Stop generating" : "Send message"}
            data-testid="composer-send-button"
            onClick={isStreaming ? onCancel : onSend}
            className="shrink-0 rounded-full"
          >
            {isStreaming ? (
              <X className="size-3.5" aria-hidden="true" />
            ) : (
              <ArrowUp className="size-3.5" aria-hidden="true" />
            )}
          </Button>
        </div>

        <div className="flex w-full items-center gap-2">
          {/* Each pill is its own menu trigger. One DropdownMenu with two
              triggers is not a thing — they open different lists. */}
          <AgentMenu
            mode="context"
            contextFiles={contextFiles}
            onSelectContext={onSelectContext}
            onBrowseContext={onBrowseContext}
          >
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              aria-label="Add context"
              data-testid="composer-attach-button"
              className={cn("h-8 border-border-subtle text-text-secondary", rail ? "px-2" : "px-3")}
            >
              <AtSign className="size-3.5" aria-hidden="true" />
              {rail ? null : "Add context"}
            </Button>
          </AgentMenu>
          <AgentMenu mode="commands" commands={commands} onSelectCommand={onSelectCommand}>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              aria-label="Commands"
              data-testid="composer-menu-button"
              className={cn("h-8 border-border-subtle text-text-secondary", rail ? "px-2" : "px-3")}
            >
              <CommandIcon className="size-3.5" aria-hidden="true" />
              {rail ? null : "Commands"}
            </Button>
          </AgentMenu>
          <span className="min-w-0 flex-1" />
          <AgentContextMeter usage={usage} turnCount={turnCount} onCompact={onCompact} />
        </div>
      </div>
    </BorderBeam>
  );
}
