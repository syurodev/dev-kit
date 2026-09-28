import * as React from "react";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { useReducedMotion } from "framer-motion";
import { AtSign, Check, ChevronDown, Command as CommandIcon, Pencil, Play, SlidersHorizontal, Target, X } from "lucide-react";
// Icon DATA, not components: morphicons interpolates path geometry, which a
// lucide-react component does not expose. Same version as lucide-react.
import { ArrowUp, Square } from "lucide";
import { MorphIcon } from "morphicons/react";
import { BorderBeam } from "border-beam";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
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
import { MENU_ROW } from "@/components/ui/menu-row";
import { Command, CommandItem, CommandList } from "@/components/ui/command";
import { commandName, commandQuery, filterCommands } from "../slashCommands";
import { VendorIcon } from "./VendorIcon";
import { onThemeChange } from "@/theme/themeColors";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentRefChip } from "../transcript/AgentRefChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";
import { AgentContextMeter } from "./AgentContextMeter";
import type { AvailableCommand, ConfigOption, ConfigSelectOption, SessionGoal, SessionMode, Usage } from "../types";
import type { QueuedAgentPrompt } from "@/stores/agentSessions";
import type { AgentImageAttachment } from "@/bridge/agenthub";
import { getAgentSandbox } from "@/bridge/preferences";
import type { AgentSandboxSettings } from "../../../../bindings/devkit/internal/preferences/models";
import type { EntityRef } from "@/workbench/intents";
import { vendorInfo } from "../vendors";

export type ComposerSize = "wide" | "rail";
export type ComposerState = "idle" | "streaming" | "attached";

/**
 * The modes the picker offers — only what the agent advertised. A fixed
 * default / acceptEdits / plan / bypassPermissions list offered modes an agent
 * may not have. The session's own mode list (ACP `modes.availableModes`, from
 * session/new and session/load) is the full answer, in the agent's order; an
 * agent that lists none falls back to the current mode plus whatever a mode
 * config option lists. The mode option is found either way, so the settings
 * menu does not offer it a second time.
 */
export function composerModeChoices(
  mode: string | null,
  configOptions: ConfigOption[],
  availableModes: SessionMode[] = [],
) {
  const modeOption = configOptions.find((option) => {
    const id = option.id.toLowerCase();
    return option.category === "mode" || id.includes("permission") || id === "mode" || id.endsWith("_mode");
  });
  const current = mode ?? (typeof modeOption?.currentValue === "string" ? modeOption.currentValue : null);
  if (availableModes.length > 0) {
    const choices: ConfigSelectOption[] = availableModes.map(({ id, name, description }) =>
      description ? { value: id, name, description } : { value: id, name },
    );
    return { modeOption, current, choices };
  }
  const listed = modeOption?.options ?? [];
  const choices: ConfigSelectOption[] = [
    ...(current ? [listed.find((choice) => choice.value === current) ?? { value: current }] : []),
    ...listed.filter((choice) => choice.value !== current),
  ];
  return { modeOption, current, choices };
}

/** Picks a mode, holding one that skips permissions for the confirmation. */
export function selectComposerMode(
  choice: ConfigSelectOption,
  onSetMode: (mode: string) => void,
  ask: (apply: () => void) => void,
): void {
  requestComposerChoice(isPermissionBypassChoice(choice), () => onSetMode(choice.value), ask);
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

/**
 * The line the bypass confirmation adds when the jail is widened (Settings →
 * Agents → Sandbox): with prompts off, nothing stands between the agent and
 * what the sandbox lets it reach, and the two widenings reach a lot — the
 * Docker socket is a way out of the jail altogether. They stay on because
 * DevKit is useless for many workflows without them (Codex spec §7.1, §8.3);
 * the person is told instead. Unset reads as on, like the setting itself, and
 * so does settings that could not be read: the warning errs towards showing.
 *
 * The settings read are today's, but a session's jail was fixed when it
 * started (SandboxExtras is read at spawn). So when either is off now, the
 * line also says that a session started with more open still has it —
 * otherwise turning Docker off mid-session would silence the warning while
 * the process kept the socket.
 */
export function bypassSandboxWarning(
  settings: Pick<AgentSandboxSettings, "shareHome" | "allowDocker"> | null | undefined,
): string {
  const home = settings?.shareHome !== false;
  const docker = settings?.allowDocker !== false;
  const tail = "the sandbox does not stop the agent from using";
  const started =
    "Sandbox settings apply when a session starts: if this one started with more open, it still has it.";
  if (home && docker) return `The Docker socket and your $HOME are open to this session — ${tail} them.`;
  if (docker) return `The Docker socket is open to this session — ${tail} it. ${started}`;
  if (home) return `Your $HOME is open to this session — ${tail} it. ${started}`;
  return started;
}

/** Applies a choice, or hands a bypass to `ask` to apply only once confirmed. */
export function requestComposerChoice(isBypass: boolean, apply: () => void, ask: (apply: () => void) => void): void {
  if (isBypass) ask(apply);
  else apply();
}

/**
 * Holds a choice picked in a menu until that menu has closed. Opened straight
 * from onSelect, the confirmation mounted while the menu was still animating
 * out: the dialog's own copy of Radix's layer stack saved the menu's
 * `pointer-events: none` on <body> as the value to restore, so Cancel could
 * leave the window unclickable, and its focus return aimed at the menu item
 * that no longer existed instead of the trigger. Released from the menu's
 * onCloseAutoFocus, the menu's layer is gone and focus is back on its trigger.
 */
export function heldChoice() {
  let held: (() => void) | null = null;
  return {
    hold: (apply: () => void) => {
      held = apply;
    },
    release: (open: (apply: () => void) => void) => {
      const apply = held;
      held = null;
      if (apply) open(apply);
    },
  };
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

/** The open slash-command list, as the textarea's key handler sees it. */
export type ComposerCommandList = {
  open: boolean;
  /** Moves the highlight by one, wrapping at both ends. */
  onMove: (delta: number) => void;
  onAccept: () => void;
  onClose: () => void;
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
  {
    streaming,
    onSend,
    onCancel,
    commandList,
  }: {
    streaming: boolean;
    onSend: () => void;
    onCancel: () => void;
    /** The slash-command list, while it is open over the field. */
    commandList?: ComposerCommandList;
  },
): void {
  const composing = Boolean(e.nativeEvent.isComposing) || e.keyCode === 229;
  if (composing) return;
  // An open list owns these keys, and has to be asked before anything else:
  // Enter picks the command instead of sending a half-typed "/comp", and
  // Escape closes the list instead of cancelling the turn that is running
  // behind it. Both would otherwise do something the user cannot undo.
  if (commandList?.open) {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        commandList.onMove(1);
        return;
      case "ArrowUp":
        e.preventDefault();
        commandList.onMove(-1);
        return;
      case "Enter":
      case "Tab":
        if (e.key === "Enter" && e.shiftKey) break; // Shift+Enter is still a newline
        e.preventDefault();
        commandList.onAccept();
        return;
      case "Escape":
        e.preventDefault();
        commandList.onClose();
        return;
    }
  }
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
  /**
   * What the session is working towards (the goal extension). The agent keeps
   * it, not the composer: this is the vendor's last word on it, and onSetGoal
   * only asks for a change.
   */
  goal?: SessionGoal | null;
  onSetGoal?: (objective: string) => void;
  turnCount?: number;
  vendor?: string;
  version?: string;
  attachedFiles?: string[];
  onRemoveAttachment?: (path: string) => void;
  imageAttachments?: AgentImageAttachment[];
  onRemoveImage?: (id: string) => void;
  /** Cross-module refs an `agent.attach` intent handed this turn. */
  refs?: EntityRef[];
  onRemoveRef?: (ref: EntityRef) => void;
  onPasteImage?: (file: File) => void;
  onDropImage?: (file: File) => void;
  imageError?: string;
  queuedPrompts?: QueuedAgentPrompt[];
  /** A turn failed or was cancelled; the queue goes on only when Resume is pressed. */
  queuePaused?: boolean;
  /** Why the queue paused, when a refusal said; shown next to Paused. */
  queuePausedReason?: string;
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
  /** The session's advertised modes; the picker lists these when there are any. */
  availableModes?: SessionMode[];
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
 * While a turn runs, a pulsing beam blooms outward around the whole input —
 * the composer itself, not the send/stop control (the glyph morph there
 * already says the button changed what it does). Mono, so it reads as system
 * chrome rather than a per-vendor colour, and follows the app's own
 * dark/light theme; the library stills itself under Reduce Motion.
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
  goal,
  onSetGoal,
  turnCount = 0,
  vendor = "",
  attachedFiles = [],
  onRemoveAttachment,
  imageAttachments = [],
  onRemoveImage,
  refs = [],
  onRemoveRef,
  onPasteImage,
  onDropImage,
  imageError,
  queuedPrompts = [],
  queuePaused = false,
  queuePausedReason,
  onResumeQueued,
  onCancelQueued,
  onEditQueued,
  onSelectCommand,
  onSelectContext,
  onBrowseContext,
  onCompact,
  agents = [],
  agent = "",
  onAgentChange,
  commands = [],
  contextFiles = [],
  configOptions = [],
  mode = null,
  availableModes = [],
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

  // The slash-command list. What is typed decides whether it is open, so there
  // is no "opened" flag to fall out of step with the field — only the highlight
  // is state, and it is reset by the query it belongs to. dismissedQuery is the
  // one thing typing cannot express: Escape has to be able to put the list away
  // without also deleting what was typed.
  const [dismissedQuery, setDismissedQuery] = useState<string | null>(null);
  const [highlight, setHighlight] = useState(0);
  const query = disabled ? null : commandQuery(value);
  const matches = query === null ? [] : filterCommands(commands, query);
  const commandListOpen = query !== null && query !== dismissedQuery && matches.length > 0;
  useEffect(() => {
    setHighlight(0);
  }, [query]);
  const highlighted = matches[Math.min(highlight, matches.length - 1)];

  function acceptCommand(command: AvailableCommand | undefined) {
    if (!command) return;
    setDismissedQuery(null);
    onSelectCommand?.(command.name);
    textareaRef.current?.focus();
  }

  const isStreaming = streaming || state === "streaming";
  const effectiveState: ComposerState =
    state ??
    (isStreaming
      ? "streaming"
      : attachedFiles.length > 0 || imageAttachments.length > 0 || refs.length > 0
        ? "attached"
        : "idle");
  const { modeOption, current: currentMode, choices: modeChoices } = composerModeChoices(mode, configOptions, availableModes);
  // Advertised modes are named by the agent; a mode config option's values
  // stay shown as they were.
  const modeLabel = (choice: ConfigSelectOption) => (availableModes.length > 0 && choice.name) || choice.value;
  const currentModeChoice = modeChoices.find((choice) => choice.value === currentMode);
  const currentModeLabel = currentModeChoice ? modeLabel(currentModeChoice) : currentMode;
  // A bypass waiting on the dialog. Not window.confirm: WKWebView (macOS) has
  // no confirm panel, so it answered false and bypass could never be enabled.
  const [pendingBypass, setPendingBypass] = useState<{ apply: () => void } | null>(null);
  // Read when the confirmation opens, not before: the setting can change in
  // Settings between one bypass and the next.
  const [sandboxWarning, setSandboxWarning] = useState("");
  useEffect(() => {
    if (!pendingBypass) return;
    let live = true;
    getAgentSandbox()
      .then((settings) => live && setSandboxWarning(bypassSandboxWarning(settings)))
      .catch(() => live && setSandboxWarning(bypassSandboxWarning(null)));
    return () => {
      live = false;
    };
  }, [pendingBypass]);
  // Picked in a menu, so it waits for that menu to close; see heldChoice.
  const [heldBypass] = useState(heldChoice);
  /**
   * Which of the composer's own menus is open, if any. One piece of state
   * rather than one per menu, and every menu is controlled by it: opening one
   * closes the rest by construction.
   *
   * Radix dismisses a menu when the pointer goes down outside it, which is
   * normally enough — but the composer floats over the transcript in a
   * pointer-events-none layer that re-enables them on itself, and there the
   * dismissal did not reach: three menus could sit open at once, stacked over
   * each other.
   */
  const [openMenu, setOpenMenu] = useState<"" | "agent" | "context" | "commands" | "mode" | "config" | "usage">("");
  const menu = (name: "agent" | "context" | "commands" | "mode" | "config" | "usage") => ({
    open: openMenu === name,
    onOpenChange: (open: boolean) => setOpenMenu(open ? name : ""),
  });
  const [goalOpen, setGoalOpen] = useState(false);
  const [goalDraft, setGoalDraft] = useState("");
  const askBypass = heldBypass.hold;
  const openHeldBypass = () => heldBypass.release((apply) => setPendingBypass({ apply }));
  const selectableOptions = configOptions.filter(
    (option) => option !== modeOption && option.options && option.options.length > 0,
  );

  const inputPlaceholder =
    placeholder ??
    (isStreaming
      ? `${agentName ?? "Agent"} is working…`
      : `Ask ${agentName ?? "agent"}…`);

  return (
    <BorderBeam
      active={isStreaming && !reduce}
      size="pulse-outside"
      colorVariant="mono"
      theme={theme}
    >
      <div
        role="region"
        aria-label="Agent prompt composer"
        data-testid="agent-composer"
        data-size={size}
        data-state={effectiveState}
        className={cn(
          // relative: the slash-command list hangs off the top of this box.
          "glass-float glass-float-low relative flex w-full flex-col gap-2 rounded-xl p-2",
          // The rail's composer fills its panel, which the user sizes.
          !rail && "max-w-274",
          className,
        )}
      >
        {attachedFiles.length > 0 || imageAttachments.length > 0 || refs.length > 0 ? (
          <div data-testid="composer-attachments" className="flex w-full flex-wrap items-center gap-1 px-1">
            {refs.map((ref) => (
              <AgentRefChip
                key={`${ref.module}/${ref.kind}/${ref.id}`}
                entityRef={ref}
                onRemove={onRemoveRef ? () => onRemoveRef(ref) : undefined}
              />
            ))}
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
              <div data-testid="composer-queue-paused" className="flex min-w-0 max-w-full items-center gap-1 text-caption-11 text-text-secondary">
                {/* One line: a long reason truncates and reads in full on hover,
                    and Resume stays beside it. */}
                <span className="min-w-0 truncate" title={queuePausedReason || undefined}>
                  {queuePausedReason ? `Paused: ${queuePausedReason}` : "Paused"}
                </span>
                {onResumeQueued ? (
                  <Button type="button" variant="ghost" size="icon-sm" aria-label="Resume queued prompts" className="shrink-0" onClick={onResumeQueued}>
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

        {/* The list of commands the agent advertised, over the field while one
            is being typed. Anchored to the composer rather than shown in a
            popover: the field keeps focus throughout — the user is still
            typing the name — and a popover would take it. onMouseDown is
            prevented for the same reason, so clicking a command does not blur
            the field underneath. Arrow keys, Enter and Escape are handled in
            the textarea's own key handler; cmdk never sees them, so the
            highlight is driven here rather than by its own value logic. */}
        {commandListOpen ? (
          <Command
            shouldFilter={false}
            value={highlighted?.name ?? ""}
            data-testid="composer-command-list"
            onMouseDown={(e) => e.preventDefault()}
            className="absolute bottom-full left-0 z-10 mb-1 w-full max-w-full"
          >
            <CommandList>
              {matches.map((command) => (
                <CommandItem
                  key={command.name}
                  value={command.name}
                  data-testid="composer-command-item"
                  onSelect={() => acceptCommand(command)}
                >
                  <CommandIcon aria-hidden="true" />
                  <span className="shrink-0 text-code-12">{commandName(command.name)}</span>
                  {/* The description takes the free space so every hint lines
                      up on the right. Not ml-auto: CommandItem's own check is
                      already pushed that way, and two of them split the gap
                      between themselves (same trap as TabSwitcher's). */}
                  <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">
                    {command.description}
                  </span>
                  {command.input?.hint ? (
                    <span className="shrink-0 text-caption-11 text-text-tertiary">{command.input.hint}</span>
                  ) : null}
                </CommandItem>
              ))}
            </CommandList>
          </Command>
        ) : null}

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
          onKeyDown={(e) =>
            handleComposerKeyDown(e, {
              streaming: isStreaming,
              onSend,
              onCancel,
              commandList: {
                open: commandListOpen,
                onMove: (delta) =>
                  setHighlight((current) => {
                    const next = (current + delta) % matches.length;
                    return next < 0 ? next + matches.length : next;
                  }),
                onAccept: () => acceptCommand(highlighted),
                onClose: () => setDismissedQuery(query),
              },
            })
          }
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
            <DropdownMenu {...menu("agent")}>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  data-testid="composer-agent"
                  className="gap-1.5 px-2 text-text-secondary"
                >
                  <VendorIcon vendor={agent} className="size-3.5" />
                  <span className="text-body-13">{vendorInfo(agent).name}</span>
                  <ChevronDown className="size-3 text-text-tertiary" aria-hidden="true" />
                </Button>
              </DropdownMenuTrigger>
              {/* No width: the rows (200px minimum) size it. At w-48 the check
                  marking the current agent was cut in half at the edge. */}
              <DropdownMenuContent side="top" align="start">
                <DropdownMenuLabel>Agent</DropdownMenuLabel>
                {agents.map((option) => (
                  <DropdownMenuItem
                    key={option}
                    data-testid="composer-agent-option"
                    icon={<VendorIcon vendor={option} className="size-3.5" />}
                    detail={option === agent ? <Check className="size-3" aria-hidden="true" /> : undefined}
                    onSelect={() => onAgentChange(option)}
                  >
                    <span>{vendorInfo(option).name}</span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
          {/* Each pill is its own menu trigger. One DropdownMenu with two
              triggers is not a thing — they open different lists. */}
          <AgentMenu
            {...menu("context")}
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
          <AgentMenu mode="commands" commands={commands} onSelectCommand={onSelectCommand} {...menu("commands")}>
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
          {/* The objective the agent keeps working towards between turns. It
              sits with the mode rather than in the ⌘ menu because it is the
              same kind of thing: per-session state the agent applies to every
              turn, not a command you run once. */}
          {onSetGoal ? (
            <ActionTooltip label={goal ? "Change what this session is working towards" : "Set what this session works towards"}>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                data-testid="composer-goal"
                data-set={goal ? "true" : "false"}
                onClick={() => {
                  setOpenMenu("");
                  setGoalDraft(goal?.objective ?? "");
                  setGoalOpen(true);
                }}
                className={cn("gap-1 px-2", goal ? "text-text-primary" : "text-text-secondary")}
              >
                <Target className="size-3.5" aria-hidden="true" />
                {goal ? <span className="max-w-32 truncate text-body-13">{goal.objective}</span> : null}
              </Button>
            </ActionTooltip>
          ) : null}
          {onSetMode && modeChoices.length > 0 ? (
            <DropdownMenu {...menu("mode")}>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="ghost" size="sm" data-testid="composer-mode" className="gap-1 px-2 text-text-secondary">
                  <SlidersHorizontal className="size-3.5" aria-hidden="true" />
                  <span className="max-w-24 truncate text-body-13">{currentModeLabel}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-64" onCloseAutoFocus={openHeldBypass}>
                <DropdownMenuLabel>Permission mode</DropdownMenuLabel>
                {/* Descriptions come from the agent (ACP modes.availableModes) as
                    full sentences, not the short trailing text DropdownMenuItem's
                    `detail` is for — cramming one into a single h-7 row wrapped and
                    overflowed onto the rows below it. Each choice gets its own
                    two-line row instead: label, then a wrapped, muted description. */}
                {modeChoices.map((choice) => (
                  <DropdownMenuItem
                    key={choice.value}
                    data-testid="composer-mode-option"
                    bare
                    className={cn(MENU_ROW, "h-auto items-start py-1.5")}
                    onSelect={() => selectComposerMode(choice, onSetMode, askBypass)}
                  >
                    <span aria-hidden className="flex size-(--icon-14) items-center justify-center pt-0.5">
                      {choice.value === currentMode ? <Check className="size-3" aria-hidden="true" /> : null}
                    </span>
                    <span className="min-w-0 py-0.5">
                      <div className="truncate">{modeLabel(choice)}</div>
                      {choice.description ? (
                        <div className="text-caption-11 text-text-tertiary">{choice.description}</div>
                      ) : null}
                    </span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
          {onSetConfigOption && selectableOptions.length > 0 ? (
            <DropdownMenu {...menu("config")}>
              <DropdownMenuTrigger asChild>
                <ActionTooltip label="Agent settings" side="top">
                  <Button type="button" variant="ghost" size="icon-sm" data-testid="composer-config" className="text-text-secondary">
                    <SlidersHorizontal aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" className="w-64" onCloseAutoFocus={openHeldBypass}>
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
          <AgentContextMeter usage={usage} turnCount={turnCount} onCompact={onCompact} {...menu("usage")} />
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
            className="shrink-0 rounded-full"
          >
            <MorphIcon
              icon={isStreaming ? Square : ArrowUp}
              size={14}
              strokeWidth={2}
              spring="snappy"
              reducedMotion="user"
            />
          </Button>
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
              {sandboxWarning ? (
                <p data-testid="bypass-sandbox-warning" className="text-body-12 text-status-warning">
                  {sandboxWarning}
                </p>
              ) : null}
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction variant="destructive" onClick={() => pendingBypass?.apply()}>
                Skip permissions
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        {/* Setting an objective is not a turn: the agent keeps it and applies
            it to the turns that follow, so this asks and closes rather than
            sending anything. Clearing it is the same call with nothing in it. */}
        <Dialog open={goalOpen} onOpenChange={setGoalOpen}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>What is this session working towards?</DialogTitle>
            </DialogHeader>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                onSetGoal?.(goalDraft.trim());
                setGoalOpen(false);
              }}
            >
              <Input
                value={goalDraft}
                onChange={(event) => setGoalDraft(event.target.value)}
                maxLength={500}
                autoFocus
                placeholder="a green build on CI"
                aria-label="Objective"
                data-testid="composer-goal-input"
              />
              <p className="mt-2 text-caption-11 text-text-tertiary">
                The agent keeps working towards this across turns until it is cleared.
              </p>
              <DialogFooter className="mt-4">
                {goal ? (
                  <Button
                    type="button"
                    variant="ghost"
                    data-testid="composer-goal-clear"
                    onClick={() => {
                      onSetGoal?.("");
                      setGoalOpen(false);
                    }}
                  >
                    Clear goal
                  </Button>
                ) : null}
                <Button type="button" variant="ghost" onClick={() => setGoalOpen(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={!goalDraft.trim()}>
                  Set goal
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>
    </BorderBeam>
  );
}
