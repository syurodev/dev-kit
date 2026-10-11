import * as React from "react";
import { useEffect, useRef, useState } from "react";
import { AtSign, Check, ChevronDown, Command as CommandIcon, FileCode, LoaderCircle, Paperclip, Pencil, Play, Plus, ShieldCheck, SlidersHorizontal, Target, X } from "lucide-react";
// Icon DATA, not components: morphicons interpolates path geometry, which a
// lucide-react component does not expose. Same version as lucide-react.
import { ArrowUp, Square } from "lucide";
import { MorphIcon } from "morphicons/react";
import { KEYS, type Path } from "platejs";
import { getMentionOnSelectItem } from "@platejs/mention";
import { Plate, usePlateEditor } from "platejs/react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { FramedCard, FramedCardPreview } from "@/components/ui/framed-card";
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
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MENU_ROW } from "@/components/ui/menu-row";
import { Command, CommandItem, CommandList } from "@/components/ui/command";
import { commandName, commandQuery, filterCommands } from "../slashCommands";
import { VendorIcon } from "./VendorIcon";
import { AgentFileChip } from "../transcript/AgentFileChip";
import { AgentRefChip } from "../transcript/AgentRefChip";
import { AgentMenu, type ContextFileItem } from "./AgentMenu";
import { AgentContextMeter } from "./AgentContextMeter";
import { ModelSettingsPopover } from "./ModelSettingsPopover";
import { ComposerEditorKit } from "./composer/composerEditorKit";
import { filterContextFiles, mentionQueryBefore } from "./composer/mentionQuery";
import { ComposerPanelArea, type TrayPanel } from "./composer/ComposerTray";
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
  const name = /^Session [a-f0-9]{8}$/i.test(agentName ?? "") ? undefined : agentName;
  return placeholder ?? (isStreaming ? `${name ?? "Agent"} is working…` : `Ask ${name ?? "agent"}…`);
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
   * Persistent context controls (useAgentTray), above the input surface.
   * Panels and typed "@" or "/" lists occupy the separate panel area.
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
 * Agent / Composer — Figma `963:2634`, Size Wide/Rail x State Idle/Streaming/
 * Attached.
 *
 * One cluster, one surface: a single border, background and radius around
 * everything the person writes with, top to bottom —
 *
 *   1. the panel area, growing above the context header;
 *   2. a quiet context header framing the inset input surface;
 *   3. the field, then attach/mention, model/mode and Send.
 *      Attachments and queued messages precede the field.
 *
 * The panel has a divider; the field and tools share one surface because there
 * are no cards. It is an ordinary block in the layout (the caller gives it a
 * region of its own below the transcript), not a layer floating over the foot
 * of one — so nothing scrolls behind it and it needs no glass.
 *
 * Sized as a desktop control, not a hero: every action is an icon at the 28px
 * control height with its name in a tooltip, the pickers are a short label and
 * a chevron, and the field opens at one line.
 *
 * Heights are bounded from the inside: the field grows to a ceiling and then
 * scrolls, attachments and queued messages scroll in their own box, and a panel
 * scrolls past the cap `composer-panel-cap` gives it — so on a short pane the
 * rows that matter (field, tools, an answer's buttons) stay on it.
 *
 * Running is a small spinner beside the gauge, not a beam round the cluster:
 * the send button's glyph already morphs into stop, and one thing moving is
 * enough. Reduce Motion stills it.
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
   * normally enough — but the composer once floated over the transcript in a
   * pointer-events-none layer that re-enabled them on itself, and there the
   * dismissal did not reach: three menus could sit open at once, stacked over
   * each other. The controlled state stays: it also lets a pick close its menu
   * on purpose (the goal dialog, a confirmation).
   */
  const [openMenu, setOpenMenu] = useState<"" | "agent" | "context" | "more" | "mode" | "config" | "usage">("");
  const menu = (name: "agent" | "context" | "more" | "mode" | "config" | "usage") => ({
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
  // The goal dialog opens from a menu row, so it waits for that menu to close
  // (heldChoice): the same ordering the bypass confirmation needs.
  const [heldGoal] = useState(heldChoice);
  const openGoalDialog = () => {
    setOpenMenu("");
    setGoalDraft(goal?.objective ?? "");
    setGoalOpen(true);
  };
  // What the model picker says on its face, so the cluster shows which model
  // is answering without opening anything.
  const sessionPanel: TrayPanel | null = shownPanel
    ? {
        ...shownPanel,
        // Escape inside a panel puts it away and hands the keyboard back to
        // the field — the one place typing resumes.
        onDismiss: shownPanel.onDismiss
          ? () => {
              shownPanel.onDismiss?.();
              editor.tf.focus();
            }
          : undefined,
      }
    : null;
  const commandsAvailable = commands.length > 0;
  const hasAttachments = attachedFiles.length > 0 || imageAttachments.length > 0 || refs.length > 0;

  return (
    // The caller's className (AgentHubContent passes "min-w-0") sizes the whole
    // cluster within the caller's region. Only the input surface draws a border.
    <FramedCard
      data-testid="agent-composer-stack"
      data-size={size}
      className={cn(
        // overflow-y: only ever scrolls on a pane too short for even the field and
        // its tools — the last resort, so Send is reachable rather than clipped.
        "min-h-0 overflow-x-hidden overflow-y-auto",
        className,
      )}
    >
      <div className="overflow-hidden rounded-control bg-surface-base">
        <ComposerPanelArea panel={sessionPanel} className="border-b-0" />
      </div>
      {(trayStrip || usage) ? (
        <div data-testid="composer-session-tools" className="flex min-h-(--control-height-sm) min-w-0 shrink-0 flex-wrap items-center gap-1 px-(--space-inline)">
          {trayStrip}
          <div className="ml-auto flex shrink-0 items-center">
            <AgentContextMeter appearance="percentage" usage={usage} turnCount={turnCount} onCompact={onCompact} {...menu("usage")} />
          </div>
        </div>
      ) : null}
      <FramedCardPreview flush data-testid="composer-surface" className="min-h-0 shrink-0 py-0 ring-1 ring-inset ring-border-subtle transition-colors duration-(--motion-fast) focus-within:ring-status-info/30">
      <div
        role="region"
        aria-label="Agent prompt composer"
        data-testid="agent-composer"
        data-size={size}
        data-state={effectiveState}
        className="flex min-h-0 w-full flex-col gap-1 p-(--space-inline)"
      >
        {hasAttachments || queuedPrompts.length > 0 || imageError ? (
          // Their own scroll box: a long queue or a dozen attachments must not
          // take the pane from the field below them.
          <div data-testid="composer-extras" className="flex max-h-52 min-h-0 shrink flex-col gap-1 overflow-y-auto overscroll-contain">
            {hasAttachments ? (
              <div data-testid="composer-attachments" className="flex w-full shrink-0 flex-wrap items-center gap-1 px-1">
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
                  <div key={image.id} className="flex items-center gap-1 rounded-sm bg-surface-base px-1 py-1">
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
              <div data-testid="composer-queued-prompts" className="flex max-h-40 w-full shrink-0 flex-col gap-1 overflow-y-auto rounded-lg border border-border-subtle bg-surface-well/50 p-2">
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
            {imageError ? <p data-testid="composer-image-error" className="shrink-0 px-1 text-caption-11 text-status-danger">{imageError}</p> : null}
          </div>
        ) : null}

        {/* Plate, not a bare textarea: this is what "@" mentions a file inline
            and ``` becomes a highlighted, editable code block
            (composerEditorKit.ts). `variant="none"` drops Editor's own
            document padding/type size so this still reads as a plain field,
            the way the bare textarea it replaces did. */}
        <div className="flex min-w-0 items-end gap-2">
          <div className="min-w-0 flex-1">
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
                  "w-full shrink-0 overflow-y-auto bg-transparent px-2 pt-1 text-body-14 text-text-primary placeholder:text-text-tertiary outline-none disabled:opacity-60",
                  // The floor is one line; the ceiling is `composer-editor-cap[-rail]`
                  // (about eleven lines wide, seven in the rail, less on a short
                  // pane), past which the field scrolls instead of pushing the
                  // cluster up over the conversation.
                  rail ? "min-h-8 composer-editor-cap-rail" : "min-h-8 composer-editor-cap",
                )}
              />
            </Plate>
          </div>

        </div>

        {/* The tools. `@container`: what a label may cost is decided by the
            row's own width, so the 720px column shows names and the 320px rail
            shows icons — the same markup, no measuring. */}
        <div data-testid="composer-toolbar" className="@container flex w-full min-w-0 items-center gap-1">
          <div data-testid="composer-writing-tools" className="flex min-w-0 flex-1 items-center gap-1">
          <div className="flex shrink-0 items-center gap-0.5">
            {/* Everything that adds something to the turn, or is not needed often:
                attach, the agent's slash commands, the goal. Typing "/" still
                opens the command list in the panel, and its keys are unchanged. */}
            <DropdownMenu {...menu("more")}>
              <DropdownMenuTrigger asChild>
                <ActionTooltip label="Attach, commands and more" side="top">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={disabled}
                    aria-label="Attach, commands and more"
                    data-testid="composer-menu-button"
                    className="text-text-secondary"
                  >
                    <Plus aria-hidden="true" />
                  </Button>
                </ActionTooltip>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                side="top"
                align="start"
                className="w-64"
                data-testid="agent-menu"
                data-mode="more"
                onCloseAutoFocus={() => heldGoal.release((apply) => apply())}
              >
                {onBrowseContext ? (
                  <DropdownMenuItem data-testid="composer-attach-files" icon={<Paperclip aria-hidden="true" />} onSelect={onBrowseContext}>
                    Attach files…
                  </DropdownMenuItem>
                ) : null}
                {commandsAvailable ? (
                  <DropdownMenuSub>
                    <DropdownMenuSubTrigger data-testid="composer-commands-trigger" icon={<CommandIcon aria-hidden="true" />}>
                      Commands
                    </DropdownMenuSubTrigger>
                    <DropdownMenuSubContent className="max-h-80 w-80 overflow-y-auto" data-testid="composer-commands-menu">
                      {commands.map((command) => (
                        <DropdownMenuItem
                          key={command.name}
                          data-testid="composer-command-menu-item"
                          className="h-auto items-start py-1.5"
                          onSelect={() => acceptCommand(command)}
                        >
                          <span className="min-w-0">
                            <span className="block truncate text-code-12">{commandName(command.name)}</span>
                            {command.description ? (
                              <span className="block text-caption-11 text-text-tertiary">{command.description}</span>
                            ) : null}
                          </span>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuSubContent>
                  </DropdownMenuSub>
                ) : null}
                {onSetGoal ? (
                  <DropdownMenuItem
                    data-testid="composer-goal-menu"
                    icon={<Target aria-hidden="true" />}
                    onSelect={() => heldGoal.hold(openGoalDialog)}
                  >
                    {goal ? "Change goal…" : "Set a goal…"}
                  </DropdownMenuItem>
                ) : null}
                {!onBrowseContext && !commandsAvailable && !onSetGoal ? (
                  <p className="px-2 py-1.5 text-caption-11 text-text-tertiary">Nothing to add right now.</p>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
            <AgentMenu
              {...menu("context")}
              mode="context"
              contextFiles={contextFiles}
              onSelectContext={onSelectContext}
              onBrowseContext={onBrowseContext}
            >
              <ActionTooltip label="Mention a file (@)" side="top">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled}
                  aria-label="Mention a file"
                  data-testid="composer-attach-button"
                  className="text-text-secondary"
                >
                  <AtSign aria-hidden="true" />
                </Button>
              </ActionTooltip>
            </AgentMenu>
          </div>

          {/* Pickers and context controls wrap as groups on narrow panes. */}
          <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden p-0.5">
            {onAgentChange ? (
              <DropdownMenu {...menu("agent")}>
                <DropdownMenuTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    data-testid="composer-agent"
                    aria-label={`Agent: ${vendorInfo(agent).name}`}
                    className="min-w-0 shrink-0 gap-1.5 px-2 text-text-secondary"
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
                compact loading={configLoading} error={configError} onRefresh={onRefreshConfig} />
            ) : onSetConfigOption && selectableOptions.length > 0 ? (
              <DropdownMenu {...menu("config")}>
                <DropdownMenuTrigger asChild>
                  <ActionTooltip label="Agent settings" side="top">
                    <Button type="button" variant="ghost" size="icon-sm" data-testid="composer-config" className="shrink-0 text-text-secondary">
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
            {onSetMode && modeChoices.length > 0 ? (
              <DropdownMenu {...menu("mode")}>
                <DropdownMenuTrigger asChild>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    data-testid="composer-mode"
                    aria-label={`Permission mode: ${currentModeLabel ?? "unknown"}`}
                    title={`Permission mode: ${currentModeLabel ?? "unknown"}`}
                    className="h-7 min-w-0 shrink-0 gap-1 rounded-full px-2 text-text-secondary"
                  >
                    {/* A shield, not the sliders the model's fallback uses: two
                        identical settings glyphs side by side said nothing about
                        which was which. */}
                    <ShieldCheck className="size-3.5 shrink-0" aria-hidden="true" />
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
            {/* The objective the agent keeps working towards between turns. Drawn
                only once one is set, so the cluster shows what the session is
                working towards; setting or clearing one is in the "+" menu. */}
            {/* Narrow panes keep Change goal in the + menu so the model and mode fit. */}
            {onSetGoal && goal ? (
              <ActionTooltip label="Change what this session is working towards" side="top">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  data-testid="composer-goal"
                  data-set="true"
                  onClick={openGoalDialog}
                  className="min-w-0 shrink gap-1 px-2 text-text-primary @max-sm:hidden"
                >
                  <Target className="size-3.5 shrink-0" aria-hidden="true" />
                  <span className="max-w-32 truncate text-body-13 @max-sm:hidden">{goal.objective}</span>
                </Button>
              </ActionTooltip>
            ) : null}
          </div>

          </div>

          <div className="ml-auto flex shrink-0 items-center gap-1">
          {isStreaming ? (
            <ActionTooltip label="Agent is working" side="top">
              <span role="status" aria-label="Agent is working" data-testid="composer-running" className="flex size-6 items-center justify-center text-text-tertiary">
                <LoaderCircle
                  aria-hidden="true"
                  className="size-3.5 animate-spin motion-reduce:animate-none"
                  style={{ animationDuration: "1.6s" }}
                />
              </span>
            </ActionTooltip>
          ) : null}
          <Button
            type="button"
            size="icon-sm"
            disabled={disabled}
            aria-label={isStreaming ? "Stop generating" : "Send message"}
            data-testid="composer-send-button"
            onClick={isStreaming ? onCancel : onSend}
            className="size-8 shrink-0 rounded-full"
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
        </div>
      </div>
      </FramedCardPreview>
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
            <DialogContent
              className="sm:max-w-md"
              // Opened from a menu row, so there is no trigger to hand focus back
              // to: typing resumes in the field, where the person was.
              onCloseAutoFocus={(event) => {
                event.preventDefault();
                editor.tf.focus();
              }}
            >
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
    </FramedCard>
  );
}
