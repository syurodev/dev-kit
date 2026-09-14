import { useSyncExternalStore } from "react";
import { AtSign, Command as CommandIcon, ArrowUp, X } from "lucide-react";
import { BorderBeam } from "border-beam";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
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
 * Sized as a desktop control, not a hero: every action is an icon at the
 * 28px control height with its name in a tooltip, the field opens at one line
 * of 40, and the whole thing is about 90 tall. At labelled pills, a 32px send
 * circle and a 56px field it was 124 — a sixth of a laptop window spent on the
 * box you type into, covering the transcript it floats over.
 *
 * While a turn runs, a border beam travels the edge. The composer is where the
 * eye is when you have just pressed Enter, and a moving edge there says "it is
 * working" without a line of text; it is warm for Claude, matching its mark in
 * the rail. The library stills itself under Reduce Motion.
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
        className="glass-float flex w-full flex-col gap-2 rounded-xl p-2"
      >
        {attachedFiles.length > 0 ? (
          <div data-testid="composer-attachments" className="flex w-full flex-wrap items-center gap-1 px-1">
            {attachedFiles.map((path) => (
              <AgentFileChip
                key={path}
                path={path}
                onClick={onRemoveAttachment ? () => onRemoveAttachment(path) : undefined}
              />
            ))}
          </div>
        ) : null}

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
          className={cn(
            "w-full resize-none bg-transparent px-2 pt-1 text-body-14 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60",
            rail ? "h-8" : "h-10",
          )}
        />

        <div className="flex w-full items-center gap-1">
          {/* Each pill is its own menu trigger. One DropdownMenu with two
              triggers is not a thing — they open different lists. */}
          <AgentMenu
            mode="context"
            contextFiles={contextFiles}
            onSelectContext={onSelectContext}
            onBrowseContext={onBrowseContext}
          >
            <ActionTooltip label="Add context" side="top">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={disabled}
                data-testid="composer-attach-button"
                className="text-text-secondary"
              >
                <AtSign aria-hidden="true" />
              </Button>
            </ActionTooltip>
          </AgentMenu>
          <AgentMenu mode="commands" commands={commands} onSelectCommand={onSelectCommand}>
            <ActionTooltip label="Commands" side="top">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                disabled={disabled}
                data-testid="composer-menu-button"
                className="text-text-secondary"
              >
                <CommandIcon aria-hidden="true" />
              </Button>
            </ActionTooltip>
          </AgentMenu>
          <span className="min-w-0 flex-1" />
          <AgentContextMeter usage={usage} turnCount={turnCount} onCompact={onCompact} />
          <Button
            type="button"
            size="icon-sm"
            disabled={disabled}
            aria-label={isStreaming ? "Stop generating" : "Send message"}
            data-testid="composer-send-button"
            onClick={isStreaming ? onCancel : onSend}
            className="shrink-0 rounded-full"
          >
            {isStreaming ? <X aria-hidden="true" /> : <ArrowUp aria-hidden="true" />}
          </Button>
        </div>
      </div>
    </BorderBeam>
  );
}
