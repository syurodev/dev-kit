import * as React from "react";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useReducedMotion } from "framer-motion";
import { AtSign, Check, ChevronDown, Command as CommandIcon, FileCode, Pencil, Play, SlidersHorizontal, Target, X } from "lucide-react";
// Icon DATA, not components: morphicons interpolates path geometry, which a
// lucide-react component does not expose. Same version as lucide-react.
import { ArrowUp, Square } from "lucide";
import { MorphIcon } from "morphicons/react";
import { KEYS, type Path } from "platejs";
import { getMentionOnSelectItem } from "@platejs/mention";
import { Plate, usePlateEditor } from "platejs/react";
import { BorderBeam } from "border-beam";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Editor } from "@/components/ui/editor";
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
import { ModelSettingsPopover } from "./ModelSettingsPopover";
import { ComposerEditorKit } from "./composer/composerEditorKit";
import { filterContextFiles, mentionQueryBefore } from "./composer/mentionQuery";
import { ComposerTray, type TrayPanel } from "./composer/ComposerTray";
import { collectMentionKeys, deserializeComposerValue, serializeComposerValue } from "./composer/composerSerialization";
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

/**
 * The field's placeholder for its current state. A pure function, not read
 * back off the rendered field: Plate shows a placeholder only when the
 * document is genuinely empty (a real `<span data-slate-placeholder>`, not
 * an always-present `placeholder=""` HTML attribute the way a `<textarea>`'s
 * was), so a non-empty `value` in a test would no longer find this string in
 * the markup at all — checking the computation directly is what the
 * existing `commandQuery`/`composerModeChoices` pattern already does for
 * exactly this reason.
 */
export function composerDefaultPlaceholder(
  placeholder: string | undefined,
  isStreaming: boolean,
  agentName: string | undefined,
): string {
  return placeholder ?? (isStreaming ? `${agentName ?? "Agent"} is working…` : `Ask ${agentName ?? "agent"}…`);
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

/** The open list over the field — slash commands or "@" files — as its key handler sees it. */
export type ComposerCommandList = {
  open: boolean;
  /** Moves the highlight by one, wrapping at both ends. */
  onMove: (delta: number) => void;
  onAccept: () => void;
  onClose: () => void;
};

/**
 * The field's shortcuts. While an IME composition is open (Vietnamese,
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
    insideCodeBlock = false,
  }: {
    streaming: boolean;
    onSend: () => void;
    onCancel: () => void;
    /** The slash-command list, while it is open over the field. */
    commandList?: ComposerCommandList;
    /** Enter continues the code line, the way any code editor's does, instead of sending. */
    insideCodeBlock?: boolean;
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
    if (insideCodeBlock) return; // a newline in the code, not a send
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
  /**
   * Answers what "@" is being typed: the files matching a query, best first
   * (agentHubListContextFiles and its draft twin). Asked again as the query
   * changes, so a large workspace is searched rather than cut to a prefix.
   * Without it "@" filters `contextFiles` locally.
   */
  onSearchContext?: (query: string) => Promise<ContextFileItem[]>;
  /**
   * The composer tray's strip (useAgentTray): where this work is and what is
   * running under it. Given, the tray is always drawn; omitted, it is drawn
   * only while an "@" or "/" list is open.
   */
  trayStrip?: React.ReactNode;
  /** The caller's panel for the tray — a permission, a question, a list opened from a chip. A list being typed goes over it. */
  trayPanel?: TrayPanel | null;
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
  onSetConfigOption?: (optionId: string, value: string) => void | Promise<void>;
  configLoading?: boolean;
  configError?: string;
  onRefreshConfig?: () => void;
  focusRequest?: number;
  disabled?: boolean;
  placeholder?: string;
  agentName?: string;
  className?: string;
};

/**
 * The "/" and "@" lists, drawn as the composer tray's panel (ComposerTray):
 * the tray is the surface, so the list drops Command's own glass, border and
 * width. Rows have a comfortable 36px hit target, and there is no
 * trailing check: the highlight is already the row's background.
 */
const COMPOSER_LIST = "w-full max-w-none rounded-none border-0 bg-transparent";
const COMPOSER_LIST_BODY = "max-h-64 gap-1 p-2";
const COMPOSER_LIST_ROW = "h-9 gap-3 rounded-md px-2 [&_svg:not([class*='size-'])]:size-3.5";

/** The folder a listed file sits in, as the list shows it beside the name. */
function contextFileFolder(file: ContextFileItem): string {
  const shown = file.description ?? file.path;
  const name = file.name || shown.split("/").pop() || "";
  return shown.endsWith("/" + name) ? shown.slice(0, -name.length - 1) : shown === name ? "" : shown;
}

/** The node types the composer's document is read and written with. */
function composerTypes(editor: { getType: (key: string) => string }) {
  return {
    paragraph: editor.getType(KEYS.p),
    mention: editor.getType(KEYS.mention),
    codeBlock: editor.getType(KEYS.codeBlock),
    codeLine: editor.getType(KEYS.codeLine),
  };
}

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
 * already says the button changed what it does). `border-beam`'s
 * `pulse-outside`, mono so it reads as system chrome rather than a vendor's
 * colour; the library stills itself under Reduce Motion.
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
  onSearchContext,
  trayStrip,
  trayPanel = null,
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
  configLoading,
  configError,
  onRefreshConfig,
  focusRequest = 0,
  disabled = false,
  placeholder,
  agentName,
  className,
}: AgentComposerProps) {
  const rail = size === "rail";
  const theme = useAppTheme();
  const reduce = useReducedMotion() ?? false;

  // A Plate document, not a bare string: it is what lets "@" mention a file
  // as an atomic chip and ``` become a highlighted, editable code block
  // (composerEditorKit.ts). It grows with what is typed on its own —
  // contentEditable reflows naturally, unlike a <textarea>'s fixed intrinsic
  // height — so none of the old manual `scrollHeight` measuring survives;
  // `min-h-*`/`max-h-*` on <Editor> below is the floor and the ceiling past
  // which it scrolls instead.
  //
  // The prop contract stays a plain string, though: `value`/`onChange` are
  // still the string every other caller (AgentHubView's draft state, queued
  // prompts, slash commands) already reads and writes. The Plate document is
  // internal, synced out on every edit (serializeComposerValue, not
  // `editor.api.markdown.serialize` — see composerEditorKit.ts for why) and
  // synced in (deserializeComposerValue, its exact inverse) only when `value`
  // changes for a reason other than the editor's own last emission —
  // comparing against what it last emitted is what keeps ordinary typing (and
  // IME composition) from being re-parsed into a fresh document on every
  // keystroke, which would fight Slate's own state.
  //
  // A value synced in is never written back out. Two composers can hold the
  // same session's prompt (the Agent Hub tab and the companion rail, both
  // mounted), and each one's edit is the other's external change: when the
  // re-read was lossy (markdown dropped a trailing space) and echoed back,
  // the mirror trimmed every space out from under the one being typed in,
  // and reset its document — and its caret — on the way.
  const editor = usePlateEditor({
    plugins: ComposerEditorKit,
    value: (created) => deserializeComposerValue(value, composerTypes(created)) as never,
  });
  const types = composerTypes(editor);

  const lastEmittedRef = useRef<string | null>(value);
  const isExternalResetRef = useRef(false);
  // Nothing read from a string is a mention ("@label" stays text), so the
  // first document holds none.
  const mentionKeysRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    if (value === lastEmittedRef.current) return;
    lastEmittedRef.current = value;
    // Already showing it (another composer typed the same thing): replacing
    // an identical document would only throw away the caret.
    if (serializeComposerValue(editor.children, composerTypes(editor)) === value) return;
    isExternalResetRef.current = true;
    editor.tf.setValue(deserializeComposerValue(value, composerTypes(editor)) as never);
  }, [value, editor]);

  useEffect(() => {
    if (focusRequest > 0) editor.tf.focus({ edge: "end" });
  }, [focusRequest, editor]);

  function handleEditorChange() {
    const nextMention = readMention();
    setMention((prev) =>
      prev && nextMention && prev.query === nextMention.query && prev.start === nextMention.start && prev.path.join() === nextMention.path.join()
        ? prev
        : nextMention,
    );
    const current = collectMentionKeys(editor.children, types.mention);
    if (isExternalResetRef.current) {
      // The document now says what `value` says; there is nothing to report.
      // An external reset (clear-on-send, load-for-edit, the other composer)
      // also never had these mentions' chips to begin with, so it is not read
      // as removing them all.
      isExternalResetRef.current = false;
      mentionKeysRef.current = current;
      return;
    }
    // A mention's chip is deleted like any other atomic node — backspace,
    // cut, select-and-delete. When one disappears from the document, drop
    // it from attachedFiles too, so removing the visible tag is enough.
    for (const path of mentionKeysRef.current) {
      if (!current.has(path)) onRemoveAttachment?.(path);
    }
    mentionKeysRef.current = current;

    const serialized = serializeComposerValue(editor.children, types);
    // Plate reports selection moves as changes too; only text is news.
    if (serialized === lastEmittedRef.current) return;
    lastEmittedRef.current = serialized;
    onChange(serialized);
  }

  // The "@" file list — the slash-command list's twin, below. The "@query"
  // before the caret decides whether it is open (mentionQuery.ts says why it
  // is read off the text and never takes the focus); what it lists is asked
  // of onSearchContext as the query changes, the previous answer staying up
  // until the next one lands so the list does not flicker on each letter.
  const [mention, setMention] = useState<{ path: Path; start: number; end: number; query: string } | null>(null);
  const [mentionDismissedAt, setMentionDismissedAt] = useState<string | null>(null);
  const [mentionFiles, setMentionFiles] = useState<ContextFileItem[] | null>(null);
  const [mentionHighlight, setMentionHighlight] = useState(0);
  const mentionQuery = disabled ? null : mention?.query ?? null;
  // Read through refs, not effect dependencies: both are new on most renders
  // (an inline callback, a defaulted `[]`), and re-running the search on each
  // one reset the highlight under the arrow keys and asked again forever.
  const searchContextRef = useRef(onSearchContext);
  searchContextRef.current = onSearchContext;
  const contextFilesRef = useRef(contextFiles);
  contextFilesRef.current = contextFiles;
  useEffect(() => {
    setMentionHighlight(0);
    if (mentionQuery === null) {
      setMentionFiles(null);
      return;
    }
    const search = searchContextRef.current;
    if (!search) {
      setMentionFiles(filterContextFiles(contextFilesRef.current, mentionQuery));
      return;
    }
    let live = true;
    const timer = setTimeout(
      () => {
        search(mentionQuery)
          .then((files) => live && setMentionFiles(files))
          .catch(() => live && setMentionFiles([]));
      },
      mentionQuery ? 120 : 0,
    );
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [mentionQuery]);
  // Where this "@" is, so Escape puts away this one and not the next one typed.
  const mentionKey = mention ? `${mention.path.join(".")}:${mention.start}` : null;
  const mentionListOpen = mentionQuery !== null && mentionKey !== mentionDismissedAt;
  const mentionHighlighted = mentionFiles?.[Math.min(mentionHighlight, mentionFiles.length - 1)];
  // The arrow keys move a highlight cmdk does not own (the field keeps the
  // keys), so cmdk never scrolls it into view: past the eighth row it went on
  // moving out of sight.
  const mentionListRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    mentionListRef.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [mentionHighlighted]);

  /** The "@query" before the caret, if one is being typed outside a code block. */
  function readMention() {
    const selection = editor.selection;
    if (!selection || !editor.api.isCollapsed()) return null;
    if (editor.api.some({ match: { type: types.codeBlock } })) return null;
    const { path, offset } = selection.anchor;
    const leaf = editor.api.node(path)?.[0] as { text?: unknown } | undefined;
    if (typeof leaf?.text !== "string") return null;
    const found = mentionQueryBefore(leaf.text.slice(0, offset));
    return found ? { path, start: found.start, end: offset, query: found.query } : null;
  }

  function acceptMention(file: ContextFileItem | undefined) {
    if (!file || !mention) return;
    // The "@query" becomes the chip: select it, delete it, insert in its place.
    editor.tf.select({ anchor: { path: mention.path, offset: mention.start }, focus: { path: mention.path, offset: mention.end } });
    editor.tf.delete();
    getMentionOnSelectItem()(editor, { key: file.path, text: file.name || file.path });
    onSelectContext?.(file.path);
    editor.tf.focus();
  }

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
  const commandListRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    commandListRef.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [highlighted]);

  function acceptCommand(command: AvailableCommand | undefined) {
    if (!command) return;
    setDismissedQuery(null);
    onSelectCommand?.(command.name);
    editor.tf.focus();
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

  const inputPlaceholder = composerDefaultPlaceholder(placeholder, isStreaming, agentName);

  // The "/" list: the commands the agent advertised, while one is being typed.
  // In the tray rather than a popover: the field keeps focus throughout — the
  // user is still typing the name — and a popover would take it. onMouseDown
  // is prevented for the same reason, so clicking a command does not blur the
  // field underneath. Arrow keys, Enter and Escape are handled in the field's
  // own key handler; cmdk never sees them, so the highlight is driven here
  // rather than by its own value logic.
  const commandList = (
    <Command
      shouldFilter={false}
      value={highlighted?.name ?? ""}
      data-testid="composer-command-list"
      onMouseDown={(e) => e.preventDefault()}
      className={COMPOSER_LIST}
    >
      <CommandList ref={commandListRef} className={COMPOSER_LIST_BODY}>
        {matches.map((command) => (
          <CommandItem
            key={command.name}
            value={command.name}
            data-testid="composer-command-item"
            indicator={false}
            className={COMPOSER_LIST_ROW}
            onSelect={() => acceptCommand(command)}
          >
            <CommandIcon aria-hidden="true" />
            <span className="shrink-0 text-code-12">{commandName(command.name)}</span>
            {/* The description takes the free space so every hint lines up on the right. */}
            <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">{command.description}</span>
            {command.input?.hint ? (
              <span className="shrink-0 text-caption-11 text-text-tertiary">{command.input.hint}</span>
            ) : null}
          </CommandItem>
        ))}
      </CommandList>
    </Command>
  );
  // The "@" list, for the same reasons (mentionQuery.ts). "No files" is said
  // rather than showing nothing, so an "@" that finds nothing does not look
  // like an "@" that did nothing.
  const mentionList = (
    <Command
      shouldFilter={false}
      value={mentionHighlighted?.path ?? ""}
      data-testid="composer-mention-list"
      onMouseDown={(e) => e.preventDefault()}
      className={COMPOSER_LIST}
    >
      <CommandList ref={mentionListRef} className={COMPOSER_LIST_BODY}>
        {!mentionFiles?.length ? (
          <p className="flex h-7 items-center px-2 text-body-13 text-text-tertiary">
            {mentionQuery ? `No files match “${mentionQuery}”` : "No files"}
          </p>
        ) : (
          mentionFiles.map((file) => (
            <CommandItem
              key={file.path}
              value={file.path}
              data-testid="composer-mention-item"
              indicator={false}
              className={COMPOSER_LIST_ROW}
              title={file.description ?? file.path}
              onSelect={() => acceptMention(file)}
            >
              <FileCode aria-hidden="true" className="text-text-tertiary" />
              <span className="max-w-[60%] shrink-0 truncate">{file.name || file.path}</span>
              {/* The folder, not the path again: the name is already beside it. */}
              <span className="min-w-0 flex-1 truncate text-caption-11 text-text-tertiary">
                {contextFileFolder(file)}
              </span>
            </CommandItem>
          ))
        )}
      </CommandList>
    </Command>
  );
  // What the tray shows: a list being typed goes over whatever the caller
  // asked for (a permission waits in the strip meanwhile, as a chip).
  const shownPanel: TrayPanel | null =
    mentionListOpen && mentionFiles !== null
      ? { key: "mention", content: mentionList }
      : commandListOpen
        ? { key: "commands", content: commandList }
        : trayPanel;
  const [trayCompact, setTrayCompact] = useState(rail);

  return (
    // The caller's className (AgentHubContent passes "min-w-0 flex-1") sizes
    // the whole of it — tray and composer — within the caller's flex row.
    <div data-testid="agent-composer-stack" className={cn("flex min-w-0 flex-col gap-2", className)}>
      {trayStrip !== undefined || shownPanel ? (
        <ComposerTray
          strip={trayStrip}
          panel={shownPanel}
          compact={trayCompact}
          onToggleCompact={() => setTrayCompact((current) => !current)}
        />
      ) : null}
      <BorderBeam
        active={isStreaming && !reduce}
        size="pulse-outside"
        colorVariant="mono"
        theme={theme}
        // Full width of the stack above: left to size itself, BorderBeam's
        // wrapper shrank to its content and the composer rendered a third of
        // the row wide with empty space beside it.
        className="w-full"
      >
        <div
          role="region"
          aria-label="Agent prompt composer"
          data-testid="agent-composer"
          data-size={size}
          data-state={effectiveState}
          className={cn(
            "glass-float glass-float-low relative flex w-full flex-col gap-2 rounded-xl p-2",
            // The rail's composer fills its panel, which the user sizes.
            !rail && "max-w-274",
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
            <div data-testid="composer-queued-prompts" className="flex max-h-40 w-full flex-col gap-1 overflow-y-auto rounded-lg border border-border-subtle bg-surface-well/50 p-2">
              <p className="px-1 pb-1 text-caption-11-medium text-text-tertiary">Queued messages · {queuedPrompts.length}</p>
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
                <div key={queued.id} className="flex min-h-8 w-full min-w-0 items-center gap-2 rounded-md px-1 text-caption-11 text-text-secondary">
                  <span className="size-1.5 shrink-0 rounded-full bg-text-tertiary" aria-hidden />
                  <span className="min-w-0 flex-1 truncate">{queued.text || `${queued.images.length} image${queued.images.length === 1 ? "" : "s"}`}</span>
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

          {/* Plate, not a bare textarea: this is what "@" mentions a file inline
              and ``` becomes a highlighted, editable code block
              (composerEditorKit.ts). `variant="none"` drops Editor's own
              document padding/type size so this still reads as a plain field,
              the way the bare textarea it replaces did. */}
            <Plate editor={editor} onChange={handleEditorChange}>
              <Editor
                variant="none"
                disabled={disabled}
                placeholder={inputPlaceholder}
                aria-label="Agent prompt input"
                data-testid="agent-composer-textarea"
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
                onKeyDown={(e) => {
                  const insideCodeBlock = editor.api.some({ match: { type: types.codeBlock } });
                  if (e.key === "Enter" && e.shiftKey && !insideCodeBlock) {
                    // Plate registers no default behaviour for Shift+Enter (only
                    // plain Enter inserts a break); left alone it would fall to
                    // contentEditable's own unreliable native handling. Inside a
                    // code block Enter is already a plain newline either way.
                    e.preventDefault();
                    editor.tf.insertSoftBreak();
                    return;
                  }
                  if (e.key === "Escape" && mentionListOpen && !mentionFiles?.length && !e.nativeEvent.isComposing) {
                    // "No files" has nothing to pick, so the list's keys stay the
                    // field's — except Escape, which must still put it away.
                    e.preventDefault();
                    setMentionDismissedAt(mentionKey);
                    return;
                  }
                  handleComposerKeyDown(e, {
                    streaming: isStreaming,
                    onSend,
                    onCancel,
                    insideCodeBlock,
                    commandList: mentionListOpen
                      ? {
                          open: (mentionFiles?.length ?? 0) > 0,
                          onMove: (delta) =>
                            setMentionHighlight((current) => {
                              const count = mentionFiles?.length ?? 0;
                              const next = (current + delta) % count;
                              return next < 0 ? next + count : next;
                            }),
                          onAccept: () => acceptMention(mentionHighlighted),
                          onClose: () => setMentionDismissedAt(mentionKey),
                        }
                      : {
                          open: commandListOpen,
                          onMove: (delta) =>
                            setHighlight((current) => {
                              const next = (current + delta) % matches.length;
                              return next < 0 ? next + matches.length : next;
                            }),
                          onAccept: () => acceptCommand(highlighted),
                          onClose: () => setDismissedQuery(query),
                        },
                  });
                }}
                className={cn(
                  "w-full overflow-y-auto bg-transparent px-2 pt-1 text-body-14 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60",
                  // Floor is the old fixed height; ceiling is about eleven lines wide
                  // and seven in the rail, past which the field scrolls instead of
                  // pushing the composer up over the conversation.
                  rail ? "min-h-8 max-h-40" : "min-h-10 max-h-60",
                )}
              />
            </Plate>

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
            {onSetConfigOption && (selectableOptions.some((option) => option.category === "model" || option.id === "model") || onRefreshConfig) ? (
              <ModelSettingsPopover options={selectableOptions} onSelect={(id, value) => {
                const option = selectableOptions.find((option) => option.id === id);
                const choice = option?.options?.find((choice) => choice.value === value) ?? { value };
                if (isPermissionBypassChoice(choice, option)) {
                  askBypass(() => { void onSetConfigOption(id, value); });
                  setOpenMenu("");
                  return;
                }
                return onSetConfigOption(id, value);
              }} {...menu("config")} onCloseAutoFocus={openHeldBypass}
                loading={configLoading} error={configError} onRefresh={onRefreshConfig} />
            ) : onSetConfigOption && selectableOptions.length > 0 ? (
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
            <Button
              type="button"
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
    </div>
  );
}
