import React, { useEffect, useRef, useState } from "react";
import {
  ArrowUp,
  Square,
  Command as CommandIcon,
  Paperclip,
  X,
} from "lucide-react";
import { cn } from "@/lib/utils";
import type { AvailableCommand, Usage } from "../types";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";

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
  onAddAttachment?: (path: string) => void;
  onRemoveAttachment?: (path: string) => void;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  disabled?: boolean;
  placeholder?: string;
  className?: string;
};

/**
 * Agent / Composer (Figma 947:2226 / 947:2236)
 * Three-state prompt composer (Idle / Streaming / Attached) in Wide (1096px) or Rail (280px) size.
 * Single frame enclosing meta band, input, attachment chips, and actions.
 */
export function AgentComposer({
  size = "wide",
  state: explicitState,
  streaming = false,
  value,
  onChange,
  onSend,
  onCancel,
  usage,
  turnCount,
  vendor = "claude",
  version = "0.74",
  attachedFiles = [],
  onAddAttachment,
  onRemoveAttachment,
  commands = [],
  contextFiles = [],
  disabled = false,
  placeholder,
  className,
}: AgentComposerProps) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuMode, setMenuMode] = useState<"commands" | "context">("commands");
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Derived state: streaming overrides, then attached if files present, otherwise idle
  const isStreaming = explicitState === "streaming" || streaming;
  const isAttached =
    explicitState === "attached" || (!isStreaming && attachedFiles.length > 0);
  const effectiveState: ComposerState = isStreaming
    ? "streaming"
    : isAttached
      ? "attached"
      : "idle";

  const isRail = size === "rail";

  // Auto-expand textarea height to fit content
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 180)}px`;
  }, [value]);

  // Close menu when clicking outside
  useEffect(() => {
    if (!menuOpen) return;
    function handleClickOutside(event: MouseEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setMenuOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [menuOpen]);

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      if (!isStreaming && value.trim()) {
        onSend();
      }
    }
  }

  // Cost and turns display
  const turns = turnCount ?? 0;
  const cost = usage?.amount ? `$${usage.amount.toFixed(4)}` : "$0.00";
  const vendorDisplay =
    vendor.charAt(0).toUpperCase() + vendor.slice(1);
  const metaRight = version ? `${vendorDisplay} · ${version}` : vendorDisplay;

  return (
    <div
      ref={containerRef}
      role="region"
      aria-label="Agent prompt composer"
      data-testid="agent-composer"
      data-size={size}
      data-state={effectiveState}
      className={cn(
        "relative flex flex-col rounded-xl border border-border-subtle bg-surface-base shadow-sm transition-colors focus-within:border-action-primary/60",
        isRail ? "w-full min-w-[260px] max-w-[320px]" : "w-full max-w-[1096px]",
        className,
      )}
    >
      {/* 1. Meta band inside the top of frame */}
      <div className="flex h-6 items-center justify-between px-3 pt-1 border-b border-border-subtle/40 select-none">
        <span
          data-testid="composer-meta-left"
          className="text-code-11 text-text-tertiary"
        >
          {`${turns} ${turns === 1 ? "turn" : "turns"} · ${cost}`}
        </span>
        <span
          data-testid="composer-meta-right"
          className="text-caption-11 text-text-tertiary"
        >
          {metaRight}
        </span>
      </div>

      {/* 2. Attached files row (if state=attached or attachedFiles exist) */}
      {isAttached && attachedFiles.length > 0 ? (
        <div
          data-testid="composer-attached-row"
          className="flex flex-wrap items-center gap-1.5 px-3 pt-2"
        >
          {attachedFiles.map((file) => (
            <div key={file} className="flex items-center gap-1">
              <AgentFileChip path={file} />
              {onRemoveAttachment ? (
                <button
                  type="button"
                  data-testid={`remove-attachment-${file}`}
                  onClick={() => onRemoveAttachment(file)}
                  className="rounded-full p-0.5 text-text-tertiary hover:bg-surface-tinted hover:text-text-primary cursor-pointer"
                  aria-label={`Remove ${file}`}
                >
                  <X className="size-3" aria-hidden="true" />
                </button>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}

      {/* 3. Input textarea */}
      <div className="flex min-h-[56px] flex-1 px-3 py-1.5">
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={disabled || isStreaming}
          rows={1}
          placeholder={
            placeholder ??
            (isStreaming
              ? "Agent is responding..."
              : isRail
                ? "Message agent..."
                : "Ask agent to build, analyze or execute...")
          }
          aria-label="Agent prompt input"
          data-testid="agent-composer-textarea"
          className="w-full resize-none bg-transparent text-body-13 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60"
        />
      </div>

      {/* 4. Bottom action bar */}
      <div className="flex h-9 items-center justify-between px-2 pb-1.5">
        {/* Left controls: menu & attachments */}
        <div className="flex items-center gap-1">
          <button
            type="button"
            data-testid="composer-menu-button"
            onClick={() => {
              setMenuMode("commands");
              setMenuOpen((prev) => !prev);
            }}
            disabled={disabled || isStreaming}
            title="Available Commands (/)"
            aria-label="Commands Menu"
            className="flex size-7 items-center justify-center rounded-control text-text-tertiary transition-colors hover:bg-surface-tinted hover:text-text-primary cursor-pointer disabled:opacity-40"
          >
            <CommandIcon className="size-3.5 shrink-0" aria-hidden="true" />
          </button>

          <button
            type="button"
            data-testid="composer-attach-button"
            onClick={() => {
              setMenuMode("context");
              setMenuOpen((prev) => !prev);
            }}
            disabled={disabled || isStreaming}
            title="Attach Context File"
            aria-label="Attach Context"
            className="flex size-7 items-center justify-center rounded-control text-text-tertiary transition-colors hover:bg-surface-tinted hover:text-text-primary cursor-pointer disabled:opacity-40"
          >
            <Paperclip className="size-3.5 shrink-0" aria-hidden="true" />
          </button>
        </div>

        {/* Right control: Stop vs Send button */}
        <div className="flex items-center">
          {isStreaming ? (
            <button
              type="button"
              data-testid="composer-stop-button"
              onClick={onCancel}
              title="Stop Turn (Cancel)"
              aria-label="Stop Turn"
              className="flex size-7 items-center justify-center rounded-control bg-status-danger text-action-primary-foreground transition-transform active:scale-95 hover:opacity-90 cursor-pointer shadow-sm"
            >
              <Square className="size-3 shrink-0 fill-current" aria-hidden="true" />
            </button>
          ) : (
            <button
              type="button"
              data-testid="composer-send-button"
              onClick={onSend}
              disabled={disabled || !value.trim()}
              title="Send Message (Enter)"
              aria-label="Send Message"
              className="flex size-7 items-center justify-center rounded-control bg-action-primary text-action-primary-foreground transition-transform active:scale-95 hover:opacity-90 cursor-pointer disabled:opacity-40 shadow-sm"
            >
              <ArrowUp className="size-3.5 shrink-0 stroke-[2.5]" aria-hidden="true" />
            </button>
          )}
        </div>
      </div>

      {/* Popover AgentMenu */}
      {menuOpen ? (
        <div
          data-testid="composer-menu-popover"
          className="absolute bottom-11 left-2 z-50 animate-in fade-in zoom-in-95 duration-100"
        >
          <AgentMenu
            mode={menuMode}
            onModeChange={setMenuMode}
            commands={commands}
            contextFiles={contextFiles}
            onSelectCommand={(cmd) => {
              onChange(`${cmd} `);
              setMenuOpen(false);
              textareaRef.current?.focus();
            }}
            onSelectContext={(path) => {
              onAddAttachment?.(path);
              setMenuOpen(false);
            }}
            onClose={() => setMenuOpen(false)}
          />
        </div>
      ) : null}
    </div>
  );
}
