import * as React from "react";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { useReducedMotion } from "framer-motion";
import { AtSign, Check, ChevronDown, Command as CommandIcon, Pencil, Play, SlidersHorizontal, X } from "lucide-react";
// Icon DATA, not components: morphicons interpolates path geometry, which a
// lucide-react component does not expose. Same version as lucide-react.
import { ArrowUp, Square } from "lucide";
import { MorphIcon } from "morphicons/react";
import { BorderBeam } from "border-beam";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { VendorIcon } from "./VendorIcon";
import { onThemeChange } from "@/theme/themeColors";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";
import { AgentContextMeter } from "./AgentContextMeter";
import type { AvailableCommand, ConfigOption, Usage } from "../types";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { AgentImageAttachment } from "@/bridge/agenthub";

export type ComposerSize = "wide" | "rail";
export type ComposerState = "idle" | "streaming" | "attached";

/**
 * The modes the picker offers: the session's current mode and whatever a mode
 * config option lists — only what the agent advertised. A fixed default /
 * acceptEdits / plan / bypassPermissions list offered modes an agent may not
 * have. ACP's session/new `modes.availableModes` would be the full list, but
 * the hub does not forward it yet (internal/acp SessionLoadResult keeps it raw).
 */
export function composerModeChoices(mode: string | null, configOptions: ConfigOption[]) {
  const modeOption = configOptions.find((option) => {
    const id = option.id.toLowerCase();
    return option.category === "mode" || id.includes("permission") || id === "mode" || id.endsWith("_mode");
  });
  const current = mode ?? (typeof modeOption?.currentValue === "string" ? modeOption.currentValue : null);
  const choices = Array.from(
    new Set([current, ...(modeOption?.options?.map((choice) => choice.value) ?? [])].filter((c): c is string => Boolean(c))),
  );
  return { modeOption, current, choices };
}

const BYPASS = /bypass|dangerously|skip[\s_-]*permissions?/i;
const OFF = /^(false|off|no|disabled?|none)$/i;

/**
 * Whether choosing this turns off the agent's permission prompts — by what the
 * value means, not by which menu lists it: bypassPermissions in the mode
 * picker, and just as much a "dangerously skip permissions" value in the
 * settings menu. An option that is itself the bypass switch counts unless the
 * value turns it off.
 */
export function isPermissionBypassChoice(
  choice: { value: string; name?: string },
  option?: { id: string; name?: string },
): boolean {
  if (BYPASS.test(choice.value) || BYPASS.test(choice.name ?? "")) return true;
  return Boolean(option && (BYPASS.test(option.id) || BYPASS.test(option.name ?? "")) && !OFF.test(choice.value));
}

/** Applies a choice, or hands a bypass to `ask` to apply only once confirmed. */
export function requestComposerChoice(isBypass: boolean, apply: () => void, ask: (apply: () => void) => void): void {
  if (isBypass) ask(apply);
  else apply();
}

export function shouldCancelComposerOnEscape(key: string, streaming: boolean, composing = false): boolean {
  return key === "Escape" && streaming && !composing;
}

type ComposerKeyEvent = {
  key: string;
  shiftKey: boolean;
  keyCode: number;
  nativeEvent: { isComposing?: boolean };
  preventDefault: () => void;
};

/**
 * The textarea's shortcuts. While an IME composition is open (Vietnamese,
 * Japanese, Chinese input) Enter picks the candidate and Escape drops it, so
 * neither may send or cancel the turn. WebKit — the webview on macOS — reports
 * the keydown that commits a composition as keyCode 229 with isComposing
 * already false, so both are checked.
 */
export function handleComposerKeyDown(
  e: ComposerKeyEvent,
  { streaming, onSend, onCancel }: { streaming: boolean; onSend: () => void; onCancel: () => void },
): void {
  const composing = Boolean(e.nativeEvent.isComposing) || e.keyCode === 229;
  if (composing) return;
  if (shouldCancelComposerOnEscape(e.key, streaming, composing)) {
    e.preventDefault();
    onCancel();
    return;
  }
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    onSend();
  }
}

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
  imageAttachments?: AgentImageAttachment[];
  onRemoveImage?: (id: string) => void;
  onPasteImage?: (file: File) => void;
  onDropImage?: (file: File) => void;
  imageError?: string;
  queuedPrompts?: QueuedAgentPrompt[];
  /** A turn failed or was cancelled; the queue goes on only when Resume is pressed. */
  queuePaused?: boolean;
  onResumeQueued?: () => void;
  onCancelQueued?: (id: string) => void;
  onEditQueued?: (prompt: QueuedAgentPrompt) => void;
  onSelectCommand?: (name: string) => void;
  onSelectContext?: (path: string) => void;
  onBrowseContext?: () => void;
  /** Sends /compact from the context popover. Omit when the agent offers no such command. */
  onCompact?: () => void;
  /**
   * The agent picker, shown only while a session is being started. A session
   * is bound to its agent's process once created, so afterwards there is
   * nothing to pick — the header says which agent it is.
   */
  agents?: string[];
  agent?: string;
  onAgentChange?: (agent: string) => void;
  commands?: AvailableCommand[];
  contextFiles?: ContextFileItem[];
  configOptions?: ConfigOption[];
  mode?: string | null;
  onSetMode?: (mode: string) => void;
  onSetConfigOption?: (optionId: string, value: string) => void;
  focusRequest?: number;
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
 * While a turn runs, a border beam travels the stop button — the control that
 * acts on the running turn, and where the pointer already is after sending.
 * Around the whole composer it was a moving edge around a field nobody is
 * typing in; on the button it points at the one thing you can do. Warm for
 * Claude, matching its mark in the rail; the library stills itself under
 * Reduce Motion.
 *
 * The glyph morphs between send and stop (morphicons) instead of cutting from
 * one to the other: the button stays where it is and changes what it does, and
 * a morph is how that reads as the same control. It honours Reduce Motion
 * (`reducedMotion="user"`) and swaps instantly then.
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
  imageAttachments = [],
  onRemoveImage,
  onPasteImage,
  onDropImage,
  imageError,
  queuedPrompts = [],
  queuePaused = false,
  onResumeQueued,
  onCancelQueued,
  onEditQueued,
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  onCompact,
  agents = [],
  agent = "claude",
  onAgentChange,
  commands = [],
  contextFiles = [],
  configOptions = [],
  mode = null,
  onSetMode,
  onSetConfigOption,
  focusRequest = 0,
  disabled = false,
  placeholder,
  agentName,
  className,
}: AgentComposerProps) {
  const theme = useAppTheme();
  const rail = size === "rail";
  const reduce = useReducedMotion() ?? false;

  // The field grows with what is typed, from one line up to its max height,
  // then scrolls. Measured rather than `field-sizing: content`: that is
  // Chromium's, and the webview on macOS and Linux is WebKit. Reset to auto
  // first so a field that was taller can shrink back when text is deleted.
  // Not animated — this changes on every keystroke, and motion there is noise.
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fit = () => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  };
  useLayoutEffect(fit, [value, rail]);
  useEffect(() => {
    if (focusRequest > 0) textareaRef.current?.focus();
  }, [focusRequest]);
  // And again when the field's width changes. The split panel opens by
  // animating from zero width, and a field measured mid-slide wrapped its
  // placeholder a word a line — an empty composer stuck at its ceiling until
  // something was typed. Width only: the height this sets would otherwise
  // trigger the observer it is answering.
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    let width = el.clientWidth;
    const observer = new ResizeObserver(() => {
      if (el.clientWidth === width) return;
      width = el.clientWidth;
      fit();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const isStreaming = streaming || state === "streaming";
  const effectiveState: ComposerState =
    state ?? (isStreaming ? "streaming" : attachedFiles.length > 0 || imageAttachments.length > 0 ? "attached" : "idle");
  const { modeOption, current: currentMode, choices: modeChoices } = composerModeChoices(mode, configOptions);
  // A bypass waiting on the dialog. Not window.confirm: WKWebView (macOS) has
  // no confirm panel, so it answered false and bypass could never be enabled.
  const [pendingBypass, setPendingBypass] = useState<{ apply: () => void } | null>(null);
  const askBypass = (apply: () => void) => setPendingBypass({ apply });
  const selectableOptions = configOptions.filter(
    (option) => option !== modeOption && option.options && option.options.length > 0,
  );

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
          "glass-float flex w-full flex-col gap-2 rounded-xl p-2",
          // The rail's composer fills its panel, which the user sizes.
          !rail && "max-w-274",
          className,
        )}
      >
        {attachedFiles.length > 0 || imageAttachments.length > 0 ? (
          <div data-testid="composer-attachments" className="flex w-full flex-wrap items-center gap-1 px-1">
            {attachedFiles.map((path) => (
              // Removed by a named button, like an image: the whole 24px chip
              // as the control removed the file on a click that said nothing.
              <div key={path} className="flex min-w-0 items-center">
                <AgentFileChip path={path} />
                {onRemoveAttachment ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${path}`} onClick={() => onRemoveAttachment(path)}>
                    <X aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            ))}
            {imageAttachments.map((image) => (
              <div key={image.id} className="flex items-center gap-1 rounded-sm bg-surface-raised px-1 py-1">
                <img src={image.previewUrl} alt={image.name} className="size-8 rounded-sm object-cover" />
                <span className="max-w-32 truncate text-caption-11 text-text-secondary">{image.name}</span>
                {onRemoveImage ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${image.name}`} onClick={() => onRemoveImage(image.id)}>
                    <X aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}
        {queuedPrompts.length > 0 ? (
          <div data-testid="composer-queued-prompts" className="flex w-full flex-wrap items-center gap-1 px-1">
            {queuePaused ? (
              // A turn failed or was cancelled with these still waiting. They do
              // not go on their own — not even after a later turn succeeds.
              <div data-testid="composer-queue-paused" className="flex items-center gap-1 text-caption-11 text-text-secondary">
                <span>Paused</span>
                {onResumeQueued ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label="Resume queued prompts" onClick={onResumeQueued}>
                    <Play aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            ) : null}
            {queuedPrompts.map((queued) => (
              <div key={queued.id} className="flex max-w-full items-center gap-1 rounded-sm bg-(--state-soft) px-2 py-1 text-caption-11 text-text-secondary">
                <span className="shrink-0">Queued</span>
                <span className="min-w-0 truncate">{queued.text || `${queued.images.length} image${queued.images.length === 1 ? "" : "s"}`}</span>
                {onEditQueued ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label="Edit queued prompt" onClick={() => onEditQueued(queued)}>
                    <Pencil aria-hidden="true" />
                  </Button>
                ) : null}
                {onCancelQueued ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label="Cancel queued prompt" onClick={() => onCancelQueued(queued.id)}>
                    <X aria-hidden="true" />
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        ) : null}
        {imageError ? <p data-testid="composer-image-error" className="px-1 text-caption-11 text-status-danger">{imageError}</p> : null}

        {/* A bare textarea, not the shared Textarea: that one brings a border,
            a fill, a focus ring and its own radius, all of which this field
            already provides around it. Overriding six properties to get back
            to plain text is more code than plain text. */}
        <textarea
          ref={textareaRef}
          rows={1}
          value={value}
          disabled={disabled}
          placeholder={inputPlaceholder}
          aria-label="Agent prompt input"
          data-testid="agent-composer-textarea"
          onChange={(e) => onChange(e.target.value)}
          onPaste={(e) => {
            const image = Array.from(e.clipboardData.files).find((file) => file.type.startsWith("image/"));
            if (image && onPasteImage) {
              e.preventDefault();
              onPasteImage(image);
            }
          }}
          onDrop={(e) => {
            const image = Array.from(e.dataTransfer.files).find((file) => file.type.startsWith("image/"));
            if (image && onDropImage) {
              e.preventDefault();
              onDropImage(image);
            }
          }}
          onDragOver={(e) => {
            if (Array.from(e.dataTransfer.types).includes("Files")) e.preventDefault();
          }}
          onKeyDown={(e) => handleComposerKeyDown(e, { streaming: isStreaming, onSend, onCancel })}
          className={cn(
            "w-full resize-none overflow-y-auto bg-transparent px-2 pt-1 text-body-14 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60",
            // Floor is the old fixed height; ceiling is about eleven lines wide
            // and seven in the rail, past which the field scrolls instead of
            // pushing the composer up over the conversation.
            rail ? "min-h-8 max-h-40" : "min-h-10 max-h-60",
          )}
        />

        <div className="flex w-full items-center gap-1">
          {onAgentChange ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  data-testid="composer-agent"
                  className="gap-1.5 px-2 text-text-secondary"
                >
                  <VendorIcon vendor={agent} className="size-3.5" />
                  <span className="text-body-13 capitalize">{agent}</span>
                  <ChevronDown className="size-3 text-text-tertiary" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-48">
                <DropdownMenuLabel>Agent</DropdownMenuLabel>
                {agents.map((option) => (
                  <DropdownMenuItem
                    key={option}
                    data-testid="composer-agent-option"
                    icon={<VendorIcon vendor={option} className="size-3.5" />}
                    detail={option === agent ? <Check className="size-3" aria-hidden="true" /> : undefined}
                    onSelect={() => onAgentChange(option)}
                  >
                    <span className="capitalize">{option}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
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
          {onSetMode && modeChoices.length > 0 ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="ghost" size="sm" data-testid="composer-mode" className="gap-1 px-2 text-text-secondary">
                  <SlidersHorizontal className="size-3.5" aria-hidden="true" />
                  <span className="max-w-24 truncate text-body-13">{currentMode}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-48">
                <DropdownMenuLabel>Permission mode</DropdownMenuLabel>
                {modeChoices.map((choice) => (
                  <DropdownMenuItem
                    key={choice}
                    data-testid="composer-mode-option"
                    detail={choice === currentMode ? <Check className="size-3" aria-hidden="true" /> : undefined}
                    onSelect={() =>
                      requestComposerChoice(
                        isPermissionBypassChoice({
                          value: choice,
                          name: modeOption?.options?.find((option) => option.value === choice)?.name,
                        }),
                        () => onSetMode(choice),
                        askBypass,
                      )
                    }
                  >
                    {choice}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
          {onSetConfigOption && selectableOptions.length > 0 ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <ActionTooltip label="Agent settings" side="top">
                  <Button type="button" variant="ghost" size="icon-sm" data-testid="composer-config" className="text-text-secondary">
                    <SlidersHorizontal aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-64">
                <DropdownMenuLabel>Agent settings</DropdownMenuLabel>
                {selectableOptions.map((option) => (
                  <React.Fragment key={option.id}>
                    <DropdownMenuLabel className="pt-2 text-body-13">{option.name || option.id}</DropdownMenuLabel>
                    {option.options?.map((choice) => (
                      <DropdownMenuItem
                        key={`${option.id}-${choice.value}`}
                        data-testid="composer-config-option"
                        detail={String(option.currentValue) === choice.value ? <Check className="size-3" aria-hidden="true" /> : undefined}
                        onSelect={() =>
                          requestComposerChoice(
                            isPermissionBypassChoice(choice, option),
                            () => onSetConfigOption(option.id, choice.value),
                            askBypass,
                          )
                        }
                      >
                        {choice.name || choice.value}
                      </DropdownMenuItem>
                    ))}
                  </React.Fragment>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
          <span className="min-w-0 flex-1" />
          <AgentContextMeter usage={usage} turnCount={turnCount} onCompact={onCompact} />
          <BorderBeam
            active={isStreaming && !reduce}
            size="sm"
            colorVariant={vendor.toLowerCase().startsWith("claude") ? "sunset" : "ocean"}
            theme={theme}
            className="shrink-0 rounded-full"
          >
            {/* Glass, like the surface it sits on: a solid lime disc was the
                loudest thing in the window at rest, for a button that does
                nothing until there is something to send. */}
            <Button
              type="button"
              variant="glass"
              size="icon-sm"
              disabled={disabled}
              aria-label={isStreaming ? "Stop generating" : "Send message"}
              data-testid="composer-send-button"
              onClick={isStreaming ? onCancel : onSend}
              className="rounded-full"
            >
              <MorphIcon
                icon={isStreaming ? Square : ArrowUp}
                size={14}
                strokeWidth={2}
                spring="snappy"
                reducedMotion="user"
              />
            </Button>
          </BorderBeam>
        </div>
        {/* Radix focuses Cancel when an alert dialog opens, so Enter or Space
            straight after picking the item does not confirm it. */}
        <AlertDialog open={pendingBypass !== null} onOpenChange={(open) => !open && setPendingBypass(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Skip permission prompts?</AlertDialogTitle>
              <AlertDialogDescription>
                {`${agentName ?? "The agent"} will edit files and run commands without asking you first, until you change this back.`}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={() => pendingBypass?.apply()}>
                Skip permissions
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
  );
}
