import { AtSign, Command as CommandIcon, ArrowUp, X } from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";
import type { AvailableCommand, Usage } from "../types";
import { VendorIcon } from "./VendorIcon";

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
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  disabled?: boolean;
  placeholder?: string;
  agentName?: string;
  className?: string;
};

/**
 * Agent / Composer — Figma `963:2634`, six variants: Size Wide/Rail x State
 * Idle/Streaming/Attached.
 *
 * Figma's note defines the shape: "one bordered frame around the input only.
 * The meta band sits on that frame's own ground above the field, so the frame's
 * top region reads as a thick top border — it is not a header for the
 * conversation, it travels with the composer."
 *
 * So there are two boxes, not one: an outer bordered frame at radius/14 whose
 * only fill is nothing, and an inner surface/raised field at radius/10. The meta
 * line lives between them. The previous implementation drew a single box with a
 * bottom-bordered header strip inside it, which reads as a header for whatever
 * is above.
 *
 * State is small: Streaming swaps the send glyph for a stop and changes the
 * placeholder; Attached inserts a chip row at the top of the field. Nothing else
 * moves.
 *
 * Size is not: at 280px the pills lose their labels and become icon-only, the
 * send button drops 32 -> 28, the input's top row drops 56 -> 40, and both the
 * vendor string and the context counter use short forms. Figma writes
 * "claude-agent-acp 0.74" and "24,503 / 1,000,000 context" wide, "claude" and
 * "24.5k / 1M" in the rail.
 */

function shortCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1)}k`;
  return String(n);
}

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
  version,
  attachedFiles = [],
  onRemoveAttachment,
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  commands = [],
  contextFiles = [],
  disabled = false,
  placeholder,
  agentName,
  className,
}: AgentComposerProps) {
  const rail = size === "rail";
  const isStreaming = streaming || state === "streaming";
  const effectiveState: ComposerState =
    state ?? (isStreaming ? "streaming" : attachedFiles.length > 0 ? "attached" : "idle");

  const used = usage?.used ?? 0;
  const limit = usage?.size ?? 0;
  const cost = usage?.amount ?? 0;

  const contextLabel = !limit
    ? ""
    : rail
      ? `${shortCount(used)} / ${shortCount(limit)}`
      : `${used.toLocaleString()} / ${limit.toLocaleString()} context`;

  const vendorLabel = rail ? vendor : [vendor, version].filter(Boolean).join(" ");

  const inputPlaceholder =
    placeholder ??
    (isStreaming
      ? `${agentName ?? "Agent"} is working…`
      : `Ask ${agentName ?? "agent"}…`);

  return (
    <div
      role="region"
      aria-label="Agent prompt composer"
      data-testid="agent-composer"
      data-size={size}
      data-state={effectiveState}
      className={cn(
        // The outer frame carries the border and NO fill — the meta band shows
        // the ground behind the composer, which is what makes the top read as a
        // thick border rather than as a header.
        "flex w-full flex-col gap-2 rounded-lg border border-border-subtle px-1 pt-3 pb-1",
        rail ? "max-w-70" : "max-w-274",
        className,
      )}
    >
      <div className="flex w-full items-center gap-2 px-1">
        <p
          data-testid="composer-meta-left"
          className="min-w-0 flex-1 text-caption-11-medium text-text-secondary"
        >
          {`${turnCount} turn${turnCount === 1 ? "" : "s"} · $${cost.toFixed(2)}`}
        </p>
        <div
          data-testid="composer-meta-right"
          className="flex shrink-0 items-center gap-2"
        >
          <VendorIcon vendor={vendor} className="size-3 text-icon-secondary" />
          <span className="text-caption-11 text-text-tertiary whitespace-nowrap">
            {vendorLabel}
          </span>
        </div>
      </div>

      <div className="flex w-full flex-col gap-3 rounded-control bg-surface-raised p-3">
        {attachedFiles.length > 0 ? (
          <div
            data-testid="composer-attachments"
            className="flex w-full flex-wrap items-center gap-1"
          >
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
            className="h-full min-w-0 flex-1 resize-none bg-transparent text-body-13 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60"
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
              className={cn(
                "h-8 border-border-subtle text-text-secondary",
                rail ? "px-2" : "px-3",
              )}
            >
              <AtSign className="size-3.5" aria-hidden="true" />
              {rail ? null : "Add context"}
            </Button>
          </AgentMenu>
          <AgentMenu
            mode="commands"
            commands={commands}
            onSelectCommand={onSelectCommand}
          >
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={disabled}
              aria-label="Commands"
              data-testid="composer-menu-button"
              className={cn(
                "h-8 border-border-subtle text-text-secondary",
                rail ? "px-2" : "px-3",
              )}
            >
              <CommandIcon className="size-3.5" aria-hidden="true" />
              {rail ? null : "Commands"}
            </Button>
          </AgentMenu>
          <span className="min-w-0 flex-1" />
          {contextLabel ? (
            <span
              data-testid="composer-context"
              className="shrink-0 text-caption-11 text-text-tertiary whitespace-nowrap"
            >
              {contextLabel}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}
