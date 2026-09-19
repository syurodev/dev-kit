// frontend/src/components/code-editor/codeIntel.ts
import {
  Annotation,
  ChangeSet,
  Prec,
  StateEffect,
  StateField,
  type ChangeDesc,
  type Extension,
  type Text,
} from "@codemirror/state";
import {
  Decoration,
  EditorView,
  ViewPlugin,
  keymap,
  showTooltip,
  type Tooltip,
  type TooltipView,
} from "@codemirror/view";
import { detectPlatform, type Platform } from "@/lib/platform";

/**
 * A place code can be defined, implemented or used. `label`/`kind` are
 * optional because a provider may only know a path and a line (e.g. a
 * grep-backed fallback) — the picker still has to render something for that
 * location. `preview` is the source line at that location; when a provider
 * supplies it, the picker shows the line itself (with the identifier
 * emphasised) instead of just naming where it is — the point of "find
 * references" is seeing the uses, not just their addresses.
 */
export type CodeLocation = {
  path: string;
  line: number;
  label?: string;
  kind?: string;
  /** The source line at that location, for lists that show the use itself. */
  preview?: string;
};

export type CodeIntelRequest = {
  path: string;
  /** 1-based line in the SAVED document (see baseline mapping below), not necessarily the line on screen. */
  line: number;
  /** 1-based line as currently displayed. */
  displayLine: number;
  identifier: string;
};

/**
 * Every method optional: a provider declares a capability by having the
 * method, and the UI for a capability it lacks never appears (no hover
 * underline, no F12 binding doing anything, no row in the right-click menu).
 * Shaped like LSP on purpose — `textDocument/definition` takes a document
 * position and returns locations — so a future LSP-backed provider is a thin
 * adapter, not a redesign.
 */
export interface CodeIntelProvider {
  definition?(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]>;
  implementation?(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]>;
  references?(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]>;
}

/** The three lookups codeIntel() drives through one shared pipeline (see `lookup` below). */
export type CodeIntelCapability = "definition" | "implementation" | "references";

/** How each capability names itself in the transient "nothing found"/"lookup failed" message. */
const CAPABILITY_LABEL: Record<CodeIntelCapability, string> = {
  definition: "definition",
  implementation: "implementations",
  references: "references",
};

export type CodeIntelConfig = {
  path: string;
  provider: CodeIntelProvider;
  onNavigate: (location: CodeLocation, options: { beside: boolean }) => void;
};

/**
 * The identifier character class shared by every helper below that has to
 * agree on what "an identifier" is: the caret-hit-test in `identifierAt` and
 * the occurrence-highlighting in `splitPreviewSegments`. One constant so the
 * two can never quietly drift (`\p{L}\p{N}_$`, not a syntax tree — this
 * editor shows Java, Go, Python, SQL and more, and a token pattern shared by
 * all of them beats a grammar for any one). Excluding a leading digit is what
 * keeps `123` a number rather than a half-identifier — every mainstream
 * language's own identifier grammar draws the line there, so `1.5` and `0x1F`
 * still read as numbers, not identifiers that happen to start oddly.
 */
const IDENTIFIER_PATTERN = /[\p{L}_$][\p{L}\p{N}_$]*/gu;

/**
 * The identifier touching `column` in `lineText`, or null when the position
 * sits inside nothing word-like.
 *
 * Matches are treated as touching `column` when `column` sits anywhere from
 * their first character up to and including their last — i.e. the caret
 * slot right after the identifier still counts, the same way clicking just
 * past a word still selects it in most editors. That is what lets a click
 * right before the `.` in `foo.bar` resolve to `foo` instead of finding
 * nothing.
 */
export function identifierAt(lineText: string, column: number): { from: number; to: number; text: string } | null {
  for (const match of lineText.matchAll(IDENTIFIER_PATTERN)) {
    const from = match.index;
    const to = from + match[0].length;
    // Matches come out in ascending order, so once one starts past `column`
    // none before it could have reached, and none after it will either.
    if (from > column) break;
    if (column <= to) return { from, to, text: match[0] };
  }
  return null;
}

/**
 * `preview` split into runs of "matches `identifier` exactly" and "everything
 * else", using the SAME identifier grammar as `identifierAt` — so looking up
 * `toResponse` emphasises it inside `list.toResponse()` but not inside
 * `toResponseWrapper()`, where the whole token is a different identifier.
 * Pure and DOM-free on purpose: the caller turns `match: true` runs into
 * `<mark>` elements, never innerHTML.
 */
export type PreviewSegment = { text: string; match: boolean };

export function splitPreviewSegments(preview: string, identifier: string): PreviewSegment[] {
  if (!identifier || !preview) return preview ? [{ text: preview, match: false }] : [];
  const segments: PreviewSegment[] = [];
  let cursor = 0;
  for (const match of preview.matchAll(IDENTIFIER_PATTERN)) {
    if (match[0] !== identifier) continue;
    const from = match.index;
    const to = from + match[0].length;
    if (from > cursor) segments.push({ text: preview.slice(cursor, from), match: false });
    segments.push({ text: match[0], match: true });
    cursor = to;
  }
  if (cursor < preview.length) segments.push({ text: preview.slice(cursor), match: false });
  return segments;
}

/**
 * The path text for a picker row: the file this very editor has open reads
 * as "this file" rather than repeating its own path back at someone already
 * looking at it. `isCurrentFile` is returned alongside so the caller can
 * apply the subtler styling without re-deriving it from the text.
 */
export function formatLocationPath(
  location: CodeLocation,
  editorPath: string,
): { text: string; isCurrentFile: boolean } {
  const isCurrentFile = location.path === editorPath;
  return { text: `${isCurrentFile ? "this file" : location.path}:${location.line}`, isCurrentFile };
}

/** Singular/plural noun and preposition for a picker's header row, per capability. */
const CAPABILITY_NOUN: Record<CodeIntelCapability, { singular: string; preposition: "of" | "to" }> = {
  definition: { singular: "definition", preposition: "of" },
  implementation: { singular: "implementation", preposition: "of" },
  references: { singular: "reference", preposition: "to" },
};

/**
 * "12 references to toResponse" / "3 implementations of list" / "2
 * definitions of findAll" — the picker's non-selectable header row. A
 * references picker always has this header (it opens even for a single
 * result, see `lookup` below), which is exactly why it needs to say what
 * that one row IS before the person reads it.
 */
export function pickerHeading(capability: CodeIntelCapability, identifier: string, count: number): string {
  const { singular, preposition } = CAPABILITY_NOUN[capability];
  const noun = count === 1 ? singular : `${singular}s`;
  return `${count} ${noun} ${preposition} ${identifier}`;
}

/**
 * How a CodeMirror-style key binding ("Mod-Alt-F12") reads in a menu: mac
 * gets the platform's own modifier glyphs, in the conventional Option-before-
 * Shift-before-Command order, with no separator; everyone else gets the
 * Windows/Linux "Ctrl+Alt+" spelling. `platform` is a parameter rather than
 * read via `detectPlatform()` inside, so this stays pure and both branches
 * are directly testable regardless of what machine the test runs on.
 */
export function shortcutHint(key: string, platform: Platform): string {
  const parts = key.split("-");
  const base = parts.pop() ?? "";
  const hasAlt = parts.includes("Alt");
  const hasShift = parts.includes("Shift");
  const hasMod = parts.includes("Mod");
  if (platform === "mac") {
    return `${hasAlt ? "⌥" : ""}${hasShift ? "⇧" : ""}${hasMod ? "⌘" : ""}${base}`;
  }
  return [hasMod && "Ctrl", hasAlt && "Alt", hasShift && "Shift", base].filter(Boolean).join("+");
}

/**
 * Tags the ONE transaction CodeEditor.tsx dispatches to replace the whole
 * document when its `value` prop changes from outside (a different file
 * opened, a reload from disk). CodeEditor stays headless — it knows to stamp
 * this annotation on that one dispatch, not why codeIntel cares — while the
 * baseline field below treats a transaction carrying it as describing a
 * freshly-saved (or freshly-opened) document, exactly like a save does.
 */
export const externalValue = Annotation.define<boolean>();

/**
 * What a code-intel index describes: the document as it was last saved, not
 * the possibly-dirty buffer on screen. `sinceBaseline` is every edit made
 * since then, composed into one ChangeDesc; `doc` is that saved text itself.
 *
 * Keeping the full `doc` looks wasteful next to just a ChangeDesc, but Text
 * is persistent and structurally shared — an untouched document costs one
 * reference, not a copy — and there is no cheaper way to turn a mapped
 * position back into a LINE NUMBER: a ChangeDesc alone maps positions, it
 * has no line structure of its own to ask.
 */
export type CodeIntelBaseline = { doc: Text; sinceBaseline: ChangeDesc };

/** The 1-based baseline line that `pos` (a position in the CURRENT document) corresponds to. */
export function baselineLineAt(baseline: CodeIntelBaseline, pos: number): number {
  const basePos = baseline.sinceBaseline.invertedDesc.mapPos(pos);
  return baseline.doc.lineAt(basePos).number;
}

/**
 * baselineLineAt's inverse: the 1-based line in `currentDoc` that baseline
 * line `baselineLine` now falls on. A lookup result naming a location in the
 * very file whose buffer is dirty is naming a BASELINE line — the index only
 * ever describes the last saved text — so it has to be re-mapped through
 * every edit made since before it means anything to the buffer on screen.
 *
 * `baselineLine` is clamped to the baseline document's own line count first:
 * a provider is not required to only ever answer with lines that existed at
 * the baseline (in practice they always do, but a stale index answering
 * about a line since deleted should not throw).
 */
export function currentLineAt(baseline: CodeIntelBaseline, baselineLine: number, currentDoc: Text): number {
  const clampedLine = Math.min(Math.max(baselineLine, 1), baseline.doc.lines);
  const basePos = baseline.doc.line(clampedLine).from;
  const pos = Math.min(baseline.sinceBaseline.mapPos(basePos), currentDoc.length);
  return currentDoc.lineAt(pos).number;
}

const resetCodeIntelBaseline = StateEffect.define();

const baselineField = StateField.define<CodeIntelBaseline>({
  create: (state) => ({ doc: state.doc, sinceBaseline: ChangeSet.empty(state.doc.length) }),
  update(value, tr) {
    if (tr.annotation(externalValue) || tr.effects.some((effect) => effect.is(resetCodeIntelBaseline))) {
      return { doc: tr.state.doc, sinceBaseline: ChangeSet.empty(tr.state.doc.length) };
    }
    if (tr.docChanged) {
      return { doc: value.doc, sinceBaseline: value.sinceBaseline.composeDesc(tr.changes) };
    }
    return value;
  },
});

/** Call after the buffer is saved: the index will now describe this text. */
export function markCodeIntelBaseline(view: EditorView): void {
  view.dispatch({ effects: resetCodeIntelBaseline.of(null) });
}

type IdentifierRange = { from: number; to: number };

const setHoverRange = StateEffect.define<IdentifierRange | null>();

/**
 * The identifier under the pointer while Cmd/Ctrl is held, or null. A
 * transaction that changes the document invalidates whatever range this
 * held — the edit may have shifted or deleted it — so it is easier and safer
 * to drop it than to map it through the change.
 */
const hoverField = StateField.define<IdentifierRange | null>({
  create: () => null,
  update(value, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setHoverRange)) value = effect.value;
    }
    if (tr.docChanged) value = null;
    return value;
  },
  provide: (field) =>
    EditorView.decorations.from(field, (range) =>
      range
        ? Decoration.set([Decoration.mark({ class: "cm-code-intel-identifier" }).range(range.from, range.to)])
        : Decoration.none,
    ),
});

/** One row of the right-click menu, or a visual divider between groups of rows. */
export type CodeIntelMenuAction = "definition" | "definitionBeside" | "implementation" | "references" | "cut" | "copy";

export type CodeIntelMenuDescriptor =
  | { kind: "separator" }
  | { kind: "item"; action: CodeIntelMenuAction; label: string; shortcut: string };

/**
 * Whether a right-click on an identifier should open OUR menu at all, rather
 * than the browser's/OS's native one. Gated on code intelligence alone: Cut
 * and Copy ride along once the menu opens, but they never justify hijacking
 * the native menu by themselves — that menu already offers both, plus Paste
 * (which ours deliberately doesn't, see `buildCodeIntelMenu`).
 */
export function hasCodeIntelMenu(capabilities: {
  definition: boolean;
  implementation: boolean;
  references: boolean;
}): boolean {
  return capabilities.definition || capabilities.implementation || capabilities.references;
}

/**
 * The right-click menu's rows, in order, for a provider with the given
 * capabilities on a buffer that may or may not be read-only. Pure — no
 * EditorView, no closures, nothing that can only exist once a menu is
 * actually open — so every combination is directly testable: a
 * references-only provider drops the definition rows and the implementations
 * row, and a read-only buffer drops Cut.
 *
 * No Paste: reading the clipboard needs a permission a desktop webview may
 * deny outright or prompt for, and an item that sometimes silently does
 * nothing is worse than not offering it. The OS's own menu — still one
 * right-click away anywhere that isn't an identifier — already has a working
 * one.
 */
export function buildCodeIntelMenu(
  capabilities: { definition: boolean; implementation: boolean; references: boolean },
  readOnly: boolean,
  platform: Platform,
): CodeIntelMenuDescriptor[] {
  const lookups: CodeIntelMenuDescriptor[] = [];
  if (capabilities.definition) {
    lookups.push({ kind: "item", action: "definition", label: "Go to Definition", shortcut: shortcutHint("F12", platform) });
    lookups.push({
      kind: "item",
      action: "definitionBeside",
      label: "Open Definition Beside",
      shortcut: shortcutHint("Mod-Alt-F12", platform),
    });
  }
  if (capabilities.implementation) {
    lookups.push({
      kind: "item",
      action: "implementation",
      label: "Go to Implementations",
      shortcut: shortcutHint("Mod-F12", platform),
    });
  }
  if (capabilities.references) {
    lookups.push({
      kind: "item",
      action: "references",
      label: "Find References",
      shortcut: shortcutHint("Shift-F12", platform),
    });
  }

  const clipboard: CodeIntelMenuDescriptor[] = [];
  if (!readOnly) {
    clipboard.push({ kind: "item", action: "cut", label: "Cut", shortcut: shortcutHint("Mod-X", platform) });
  }
  clipboard.push({ kind: "item", action: "copy", label: "Copy", shortcut: shortcutHint("Mod-C", platform) });

  return lookups.length > 0 ? [...lookups, { kind: "separator" }, ...clipboard] : clipboard;
}

/**
 * A row a keyboard/selection model can land on: `count` total slots, some of
 * which (separators, a future disabled row) `isSelectable` rejects. Shared by
 * the picker (every `CodeLocation` is selectable) and the menu (separators
 * and disabled items are not), so ArrowUp/Down/Home/End/Enter are written
 * once instead of once per popup.
 */
export function nextSelectableIndex(
  count: number,
  from: number,
  delta: number,
  isSelectable: (index: number) => boolean,
): number {
  if (count === 0) return -1;
  let index = from;
  for (let step = 0; step < count; step++) {
    index = (index + delta + count) % count;
    if (isSelectable(index)) return index;
  }
  // Nothing else qualifies — stay where selection already was.
  return from;
}

export function firstSelectableIndex(count: number, isSelectable: (index: number) => boolean): number {
  for (let i = 0; i < count; i++) if (isSelectable(i)) return i;
  return -1;
}

export function lastSelectableIndex(count: number, isSelectable: (index: number) => boolean): number {
  for (let i = count - 1; i >= 0; i--) if (isSelectable(i)) return i;
  return -1;
}

/**
 * What the go-to-definition tooltip is showing, if anything. `onNavigate` is
 * carried on the picker's own state rather than closed over by the tooltip's
 * `create` function, because that function (below) is a single module-level
 * value shared by every editor with codeIntel attached — it has to read
 * per-editor things like `onNavigate` out of the state it is handed, not out
 * of a closure that would only ever be one editor's. The menu's rows carry
 * their own `run` closures for the same reason.
 */
type CodeIntelMenuRow =
  | { kind: "separator" }
  | {
      kind: "item";
      label: string;
      shortcut: string;
      disabled?: boolean;
      run: (view: EditorView) => void;
    };

type CodeIntelTooltipState =
  | { kind: "message"; pos: number; text: string }
  | {
      kind: "picker";
      pos: number;
      items: CodeLocation[];
      selected: number;
      beside: boolean;
      capability: CodeIntelCapability;
      identifier: string;
      editorPath: string;
      onNavigate: CodeIntelConfig["onNavigate"];
    }
  | { kind: "menu"; pos: number; rows: CodeIntelMenuRow[]; selected: number };

const setCodeIntelTooltip = StateEffect.define<CodeIntelTooltipState | null>();

const codeIntelTooltipField = StateField.define<CodeIntelTooltipState | null>({
  create: () => null,
  update(value, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setCodeIntelTooltip)) return effect.value;
    }
    // Typing counts as "the user moved on" — this is what `dismisses ... on
    // any input` means for the "no definition" message (the ~1.5s timer in
    // createCodeIntelTooltipView handles the rest of "any input" for that
    // one). Deliberately NOT keyed on `tr.selection` too: focusing the view
    // after a Cmd-click (see the mousedown handler below) can itself cause a
    // native selectionchange a tick later, which would race this exact
    // dispatch and could close a tooltip the instant it opens. "Outside
    // click" is instead handled directly, in createCodeIntelTooltipView's own
    // document-level mousedown listener, which doesn't depend on whether
    // CodeMirror turns that click into a selection change at all.
    if (tr.docChanged) return null;
    return value;
  },
  provide: (field) => showTooltip.from(field, (state) => (state ? toTooltip(state) : null)),
});

function toTooltip(state: CodeIntelTooltipState): Tooltip {
  // `create` is the SAME function reference every time (module scope, not
  // rebuilt per call): CodeMirror's tooltip manager only reuses a
  // TooltipView — calling its `update` instead of tearing it down and
  // rebuilding the DOM — when the new Tooltip's `create` is `===` the old
  // one's. Selecting the next row in the picker would otherwise flash.
  //
  // Everything shows above the anchor except the menu: a menu reads
  // downward, the way every native context menu does, so it opens below the
  // identifier that was right-clicked instead of covering it.
  return { pos: state.pos, above: state.kind !== "menu", create: createCodeIntelTooltipView };
}

/** Dispatches the transient message tooltip. Shared by lookup failures and clipboard failures. */
function showCodeIntelMessage(view: EditorView, pos: number, text: string) {
  // The document may have changed since `pos` was computed, and a tooltip
  // anchored past the document's end throws. Clamping rather than mapping:
  // an edit also closes the tooltip (see codeIntelTooltipField), so a
  // slightly-off anchor is on screen for one frame at most.
  const tooltipPos = Math.min(pos, view.state.doc.length);
  view.dispatch({ effects: setCodeIntelTooltip.of({ kind: "message", pos: tooltipPos, text }) });
}

function createCodeIntelTooltipView(view: EditorView): TooltipView {
  const dom = document.createElement("div");
  dom.className = "cm-code-intel-tooltip";
  let dismissTimer: ReturnType<typeof setTimeout> | undefined;

  function currentState(): CodeIntelTooltipState | null {
    return view.state.field(codeIntelTooltipField, false) ?? null;
  }

  function close() {
    view.dispatch({ effects: setCodeIntelTooltip.of(null) });
  }

  function renderPreview(preview: string, identifier: string): HTMLElement {
    const el = document.createElement("div");
    el.className = "cm-code-intel-option-preview";
    for (const segment of splitPreviewSegments(preview, identifier)) {
      if (segment.match) {
        const mark = document.createElement("mark");
        mark.className = "cm-code-intel-option-preview-match";
        mark.textContent = segment.text;
        el.appendChild(mark);
      } else {
        el.appendChild(document.createTextNode(segment.text));
      }
    }
    return el;
  }

  function renderPicker(state: Extract<CodeIntelTooltipState, { kind: "picker" }>) {
    const container = document.createElement("div");
    container.className = "cm-code-intel-picker";
    // Never wider than the editor showing it: this can hold dozens of rows
    // of source code now, and a fixed pixel cap would spill past a narrow
    // editor's own edge.
    container.style.maxWidth = `${Math.max(220, view.dom.clientWidth - 32)}px`;

    const heading = pickerHeading(state.capability, state.identifier, state.items.length);
    const header = document.createElement("div");
    header.className = "cm-code-intel-picker-header";
    header.textContent = heading;
    container.appendChild(header);

    const list = document.createElement("ul");
    list.className = "cm-code-intel-list";
    list.setAttribute("role", "listbox");
    list.setAttribute("aria-label", heading);
    state.items.forEach((location, index) => {
      const option = document.createElement("li");
      option.className = "cm-code-intel-option";
      option.setAttribute("role", "option");
      option.setAttribute("aria-selected", index === state.selected ? "true" : "false");
      if (index === state.selected) option.classList.add("cm-code-intel-option-selected");

      const { text: pathText, isCurrentFile } = formatLocationPath(location, state.editorPath);
      if (isCurrentFile) option.classList.add("cm-code-intel-option-current-file");

      if (location.preview) {
        option.classList.add("cm-code-intel-option-with-preview");
        option.appendChild(renderPreview(location.preview, state.identifier));

        const meta = document.createElement("div");
        meta.className = "cm-code-intel-option-meta";
        meta.textContent = location.label ? `${location.label} · ${pathText}` : pathText;
        option.appendChild(meta);
      } else {
        const label = document.createElement("span");
        label.className = "cm-code-intel-option-label";
        label.textContent = location.label ?? location.path;
        option.appendChild(label);

        if (location.kind) {
          const kind = document.createElement("span");
          kind.className = "cm-code-intel-option-kind";
          kind.textContent = location.kind;
          option.appendChild(kind);
        }

        const path = document.createElement("span");
        path.className = "cm-code-intel-option-path";
        path.textContent = pathText;
        option.appendChild(path);
      }

      // mousedown, not click: click fires after mouseup, by which point a
      // mousedown on this LI has already blurred the editor. preventDefault
      // here keeps focus (and the caret) exactly where it was.
      option.addEventListener("mousedown", (event) => {
        event.preventDefault();
        close();
        state.onNavigate(location, { beside: state.beside });
      });

      list.appendChild(option);
    });
    container.appendChild(list);
    dom.appendChild(container);

    // Keeps the selected row on screen as ArrowUp/Down/Home/End/PageUp/
    // PageDown move past the visible window of a list that can hold dozens
    // of rows. `?.` guards environments (tests) with no real layout engine.
    const selectedOption = list.children[state.selected] as HTMLElement | undefined;
    selectedOption?.scrollIntoView?.({ block: "nearest" });
  }

  function renderMenu(state: Extract<CodeIntelTooltipState, { kind: "menu" }>) {
    const menu = document.createElement("div");
    menu.className = "cm-code-intel-menu";
    menu.setAttribute("role", "menu");
    menu.setAttribute("aria-label", "Code actions");

    state.rows.forEach((row, index) => {
      if (row.kind === "separator") {
        const separator = document.createElement("div");
        separator.className = "cm-code-intel-menu-separator";
        separator.setAttribute("role", "separator");
        menu.appendChild(separator);
        return;
      }

      const item = document.createElement("div");
      item.className = "cm-code-intel-menu-item";
      item.setAttribute("role", "menuitem");
      if (row.disabled) item.setAttribute("aria-disabled", "true");
      if (index === state.selected) item.classList.add("cm-code-intel-menu-item-selected");

      const label = document.createElement("span");
      label.className = "cm-code-intel-menu-item-label";
      label.textContent = row.label;
      item.appendChild(label);

      const shortcut = document.createElement("span");
      shortcut.className = "cm-code-intel-menu-item-shortcut";
      shortcut.textContent = row.shortcut;
      item.appendChild(shortcut);

      if (!row.disabled) {
        item.addEventListener("mousedown", (event) => {
          event.preventDefault();
          close();
          row.run(view);
        });
      }

      menu.appendChild(item);
    });

    dom.appendChild(menu);
    const selectedRow = menu.children[state.selected] as HTMLElement | undefined;
    selectedRow?.scrollIntoView?.({ block: "nearest" });
  }

  function render() {
    window.clearTimeout(dismissTimer);
    const state = currentState();
    dom.replaceChildren();
    if (!state) return;

    if (state.kind === "message") {
      const message = document.createElement("div");
      message.className = "cm-code-intel-message";
      message.textContent = state.text;
      dom.appendChild(message);
      // ~1.5s self-dismiss. Guarded by identity: if a newer tooltip has
      // already replaced this one by the time the timer fires, closing
      // would wrongly clear THAT one instead of a no-op on this one.
      dismissTimer = setTimeout(() => {
        if (currentState() === state) close();
      }, 1500);
      return;
    }

    if (state.kind === "menu") {
      renderMenu(state);
      return;
    }

    renderPicker(state);
  }

  render();

  // Closes the tooltip on a mousedown anywhere outside it, editor text and app
  // chrome alike. The field deliberately does not watch selection changes (see
  // its update), so this listener is the only thing that does. Capture phase so
  // it runs before the click's own handler decides anything.
  function onOutsideMouseDown(event: MouseEvent) {
    if (!dom.contains(event.target as Node)) close();
  }
  document.addEventListener("mousedown", onOutsideMouseDown, true);

  return {
    dom,
    update(update) {
      if (update.startState.field(codeIntelTooltipField, false) !== update.state.field(codeIntelTooltipField, false)) {
        render();
      }
    },
    destroy() {
      window.clearTimeout(dismissTimer);
      document.removeEventListener("mousedown", onOutsideMouseDown, true);
    },
  };
}

/** Row count backing the selection model for whichever popup (picker or menu) is open. */
function selectableRowCount(state: CodeIntelTooltipState): number {
  if (state.kind === "picker") return state.items.length;
  if (state.kind === "menu") return state.rows.length;
  return 0;
}

/** Every picker row is selectable; a menu's separators and disabled items are not. */
function isRowSelectable(state: CodeIntelTooltipState, index: number): boolean {
  if (state.kind === "picker") return true;
  if (state.kind === "menu") {
    const row = state.rows[index];
    return row.kind === "item" && !row.disabled;
  }
  return false;
}

/** ArrowUp/ArrowDown: wraps, and skips whatever `isRowSelectable` rejects. Claims the key only while a picker or menu is open. */
function moveTooltipSelection(delta: number) {
  return (view: EditorView): boolean => {
    const state = view.state.field(codeIntelTooltipField, false);
    if (!state || (state.kind !== "picker" && state.kind !== "menu")) return false;
    const selected = nextSelectableIndex(selectableRowCount(state), state.selected, delta, (index) =>
      isRowSelectable(state, index),
    );
    if (selected !== state.selected) view.dispatch({ effects: setCodeIntelTooltip.of({ ...state, selected }) });
    return true;
  };
}

/** Home/End: jump to the first/last selectable row (clamped, not wrapped). Shared by the picker and the menu. */
function jumpTooltipSelection(
  toIndex: (state: Extract<CodeIntelTooltipState, { kind: "picker" | "menu" }>) => number,
) {
  return (view: EditorView): boolean => {
    const state = view.state.field(codeIntelTooltipField, false);
    if (!state || (state.kind !== "picker" && state.kind !== "menu")) return false;
    const index = toIndex(state);
    if (index >= 0 && index !== state.selected) {
      view.dispatch({ effects: setCodeIntelTooltip.of({ ...state, selected: index }) });
    }
    return true;
  };
}

const PICKER_PAGE_STEP = 8;

/** PageUp/PageDown: picker-only (a menu never has enough rows to page through), clamped to the ends. */
function pageTooltipSelection(delta: number) {
  return (view: EditorView): boolean => {
    const state = view.state.field(codeIntelTooltipField, false);
    if (!state || state.kind !== "picker") return false;
    const selected = Math.max(0, Math.min(state.selected + delta, state.items.length - 1));
    if (selected !== state.selected) view.dispatch({ effects: setCodeIntelTooltip.of({ ...state, selected }) });
    return true;
  };
}

function commitTooltipSelection(view: EditorView): boolean {
  const state = view.state.field(codeIntelTooltipField, false);
  if (!state) return false;
  if (state.kind === "picker") {
    const location = state.items[state.selected];
    view.dispatch({ effects: setCodeIntelTooltip.of(null) });
    state.onNavigate(location, { beside: state.beside });
    return true;
  }
  if (state.kind === "menu") {
    const row = state.rows[state.selected];
    view.dispatch({ effects: setCodeIntelTooltip.of(null) });
    if (row?.kind === "item" && !row.disabled) row.run(view);
    return true;
  }
  return false;
}

function dismissTooltip(view: EditorView): boolean {
  if (!view.state.field(codeIntelTooltipField, false)) return false;
  view.dispatch({ effects: setCodeIntelTooltip.of(null) });
  return true;
}

/** Cmd on macOS, Ctrl elsewhere — CodeMirror's own rule for "this click adds a cursor", matched here on purpose. */
function isModEvent(event: { metaKey: boolean; ctrlKey: boolean }): boolean {
  return detectPlatform() === "mac" ? event.metaKey : event.ctrlKey;
}

function identifierRangeAt(view: EditorView, point: { x: number; y: number }): IdentifierRange | null {
  const pos = view.posAtCoords(point);
  if (pos == null) return null;
  const line = view.state.doc.lineAt(pos);
  const id = identifierAt(line.text, pos - line.from);
  return id ? { from: line.from + id.from, to: line.from + id.to } : null;
}

/** The range Copy/Cut act on: the current selection when it's non-empty, otherwise the identifier under the click that opened the menu. */
function clipboardRange(view: EditorView, fallback: IdentifierRange): IdentifierRange {
  const selection = view.state.selection.main;
  return selection.empty ? fallback : { from: selection.from, to: selection.to };
}

async function performCopy(view: EditorView, range: IdentifierRange) {
  const text = view.state.sliceDoc(range.from, range.to);
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    showCodeIntelMessage(view, range.from, "Could not copy to the clipboard");
  }
}

async function performCut(view: EditorView, range: IdentifierRange) {
  const doc = view.state.doc;
  const text = view.state.sliceDoc(range.from, range.to);
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Deleting text no one could copy would be a silent data loss — the
    // buffer is left untouched and only the message explains why nothing
    // moved.
    showCodeIntelMessage(view, range.from, "Could not copy to the clipboard");
    return;
  }
  // The clipboard write is asynchronous, and `range` was measured before it.
  // If anything was typed meanwhile those offsets now name other text — so
  // the cut stops at the copy, which has already happened, rather than delete
  // whatever moved into that range.
  if (view.state.doc !== doc) return;
  // A normal dispatch, not a special "cut" path: this is what makes it
  // undoable and what runs CodeEditor's own updateListener, the same as any
  // other edit, so the buffer is marked dirty the usual way.
  view.dispatch({ changes: { from: range.from, to: range.to, insert: "" }, selection: { anchor: range.from } });
}

/**
 * Headless go-to-definition/implementation/references and its right-click
 * menu. Knows nothing about where answers come from — only `config.provider`
 * does — and adds no UI for a method the provider doesn't implement.
 */
export function codeIntel(config: CodeIntelConfig): Extension {
  // Everything that belongs to ONE editor lives on this plugin's instance, not
  // in this function's closure. The closure is per `codeIntel(config)` call,
  // and one call can power several EditorViews: the caller memoizes the result,
  // and React StrictMode's mount → destroy → mount builds two views from the
  // same extension array. A `destroyed` flag up here would be set by the view
  // that died and then read by the one that lived.
  const session = ViewPlugin.fromClass(
    class {
      // Kept even while Cmd/Ctrl is not held: without it, pressing Cmd with the
      // mouse at rest (no mousemove to react to) would show no underline until
      // the pointer moved again.
      lastPoint: { x: number; y: number } | null = null;
      pendingAbort: AbortController | null = null;
      requestSeq = 0;
      destroyed = false;

      constructor(readonly view: EditorView) {}

      destroy() {
        this.destroyed = true;
        this.pendingAbort?.abort();
      }

      updateHover(event: { metaKey: boolean; ctrlKey: boolean }) {
        if (!config.provider.definition || !this.lastPoint || !isModEvent(event)) {
          this.clearHover();
          return;
        }
        const range = identifierRangeAt(this.view, this.lastPoint);
        const current = this.view.state.field(hoverField, false) ?? null;
        const unchanged = range?.from === current?.from && range?.to === current?.to;
        if (!unchanged) this.view.dispatch({ effects: setHoverRange.of(range) });
      }

      clearHover() {
        if (this.view.state.field(hoverField, false)) this.view.dispatch({ effects: setHoverRange.of(null) });
      }

      /**
       * A provider answers in terms of the SAVED file — so a location naming
       * the file this very editor has open needs its line re-mapped through
       * whatever has been typed since the baseline before it means anything
       * on screen. Every other location is left alone: the index's line
       * already is the line on disk for a file this editor isn't editing.
       */
      resolveLocation(location: CodeLocation): CodeLocation {
        if (location.path !== config.path) return location;
        const baseline = this.view.state.field(baselineField);
        const line = currentLineAt(baseline, location.line, this.view.state.doc);
        return line === location.line ? location : { ...location, line };
      }

      /**
       * One pipeline for definition, implementation and references: abort
       * the previous in-flight lookup, tag this one with a sequence number so
       * a late result can't clobber a newer tooltip, map the click to a
       * baseline request, resolve every result back onto the screen, then
       * decide navigate vs. picker vs. message per `capability`.
       */
      async lookup(capability: CodeIntelCapability, pos: number, beside: boolean) {
        const method = config.provider[capability];
        if (!method) return;
        const { view } = this;
        const line = view.state.doc.lineAt(pos);
        const id = identifierAt(line.text, pos - line.from);
        if (!id) return;
        const anchor = line.from + id.from;

        // Abort whatever lookup was already in flight — its result would only
        // ever be stale now — and remember which request this is so a result
        // that arrives after a NEWER one started (abort is advisory; a provider
        // is not required to honour it promptly) is ignored rather than
        // clobbering the newer tooltip.
        this.pendingAbort?.abort();
        const controller = new AbortController();
        this.pendingAbort = controller;
        const seq = ++this.requestSeq;

        const request: CodeIntelRequest = {
          path: config.path,
          line: baselineLineAt(view.state.field(baselineField), anchor),
          displayLine: line.number,
          identifier: id.text,
        };

        let locations: CodeLocation[] = [];
        let failed = false;
        try {
          locations = await method(request, controller.signal);
        } catch {
          failed = true;
        }

        if (this.destroyed || seq !== this.requestSeq) return;

        // Resolved once, up front, so the tooltip's own path:line text and
        // whatever onNavigate actually receives always agree — a location in
        // the file open right now is never shown at one line and jumped to at
        // another.
        const resolved = locations.map((location) => this.resolveLocation(location));

        // References always opens the picker, even for a single hit: someone
        // asking "where is this used" wants the list, not to be teleported to
        // the first hit it happens to find.
        if (capability !== "references" && resolved.length === 1) {
          config.onNavigate(resolved[0], { beside });
          return;
        }

        if (resolved.length === 0) {
          showCodeIntelMessage(
            view,
            anchor,
            failed ? `Could not look up ${CAPABILITY_LABEL[capability]}` : `No ${CAPABILITY_LABEL[capability]} found`,
          );
          return;
        }

        view.dispatch({
          effects: setCodeIntelTooltip.of({
            kind: "picker",
            pos: Math.min(anchor, view.state.doc.length),
            items: resolved,
            selected: 0,
            beside,
            capability,
            identifier: id.text,
            editorPath: config.path,
            onNavigate: config.onNavigate,
          }),
        });
      }
    },
    {
      // Purely observational: every handler returns false, so nothing
      // CodeMirror does by default is affected by holding Cmd/Ctrl and moving
      // the mouse.
      eventHandlers: {
        mousemove(event) {
          this.lastPoint = { x: event.clientX, y: event.clientY };
          this.updateHover(event);
          return false;
        },
        keydown(event) {
          if (isModEvent(event)) this.updateHover(event);
          return false;
        },
        keyup(event) {
          if (!isModEvent(event)) this.clearHover();
          return false;
        },
        mouseleave() {
          this.lastPoint = null;
          this.clearHover();
          return false;
        },
        blur() {
          this.clearHover();
          return false;
        },
        scroll() {
          this.clearHover();
          return false;
        },
      },
    },
  );

  function lookup(view: EditorView, capability: CodeIntelCapability, pos: number, beside: boolean): boolean {
    const current = view.plugin(session);
    if (!current || !config.provider[capability]) return false;
    void current.lookup(capability, pos, beside);
    return true;
  }

  // Prec.highest: `codeIntel()` is handed to <CodeEditor extensions={[...]}>
  // like any other extension, with no say over where it lands relative to
  // basicSetup — and CodeEditor.tsx places `extensions` AFTER basicSetup.
  // Without an explicit precedence, CodeMirror's own mousedown handling
  // (which is what adds a second cursor on a Cmd/Ctrl-click) would run
  // first and this handler would never get a look at the event.
  const clickHandler = Prec.highest(
    EditorView.domEventHandlers({
      mousedown(event, view) {
        if (event.button !== 0 || !isModEvent(event) || !config.provider.definition) return false;
        const pos = view.posAtCoords({ x: event.clientX, y: event.clientY });
        if (pos == null) return false;
        const line = view.state.doc.lineAt(pos);
        if (!identifierAt(line.text, pos - line.from)) return false;
        view.plugin(session)?.clearHover();
        // Returning true (below) preventDefaults the mousedown, which also
        // cancels the browser's own focus-on-click — so without this, a picker
        // opened by clicking into an unfocused editor would take
        // Up/Down/Enter/Escape and go nowhere.
        view.focus();
        // Returning true also — per EditorView.domEventHandlers' contract —
        // skips every lower-precedence handler for this event, built-in
        // included. That is what stops CodeMirror's default "Cmd/Ctrl-click
        // adds a cursor" from firing for the same click.
        return lookup(view, "definition", pos, event.altKey);
      },

      contextmenu(event, view) {
        // The escape hatch to the browser's/OS's own menu, even sitting right
        // on an identifier — Shift+right-click is the conventional way to ask
        // for it in apps that customise the context menu.
        if (event.shiftKey) return false;

        const capabilities = {
          definition: Boolean(config.provider.definition),
          implementation: Boolean(config.provider.implementation),
          references: Boolean(config.provider.references),
        };
        if (!hasCodeIntelMenu(capabilities)) return false;

        const pos = view.posAtCoords({ x: event.clientX, y: event.clientY });
        if (pos == null) return false;
        const line = view.state.doc.lineAt(pos);
        const id = identifierAt(line.text, pos - line.from);
        // Whitespace, punctuation: nothing to look up or emphasise, so let
        // the native menu through instead of opening an empty-feeling one.
        if (!id) return false;

        view.plugin(session)?.clearHover();

        // Moves the caret to the click UNLESS it landed inside the current,
        // non-empty selection — that is what lets Copy/Cut below act on a
        // multi-line selection instead of always collapsing to one word.
        const selection = view.state.selection.main;
        const insideSelection = !selection.empty && pos >= selection.from && pos <= selection.to;
        if (!insideSelection) view.dispatch({ selection: { anchor: pos } });
        view.focus();

        const identifierRange: IdentifierRange = { from: line.from + id.from, to: line.from + id.to };
        const platform = detectPlatform();
        const descriptors = buildCodeIntelMenu(capabilities, view.state.readOnly, platform);
        const rows: CodeIntelMenuRow[] = descriptors.map((descriptor) => {
          if (descriptor.kind === "separator") return descriptor;
          const run = (v: EditorView) => {
            switch (descriptor.action) {
              case "definition":
                lookup(v, "definition", identifierRange.from, false);
                return;
              case "definitionBeside":
                lookup(v, "definition", identifierRange.from, true);
                return;
              case "implementation":
                lookup(v, "implementation", identifierRange.from, false);
                return;
              case "references":
                lookup(v, "references", identifierRange.from, false);
                return;
              case "copy":
                void performCopy(v, clipboardRange(v, identifierRange));
                return;
              case "cut":
                void performCut(v, clipboardRange(v, identifierRange));
                return;
            }
          };
          return { kind: "item", label: descriptor.label, shortcut: descriptor.shortcut, run };
        });

        const selected = firstSelectableIndex(rows.length, (index) => rows[index].kind === "item");
        view.dispatch({
          effects: setCodeIntelTooltip.of({
            kind: "menu",
            pos: identifierRange.from,
            rows,
            selected: Math.max(selected, 0),
          }),
        });
        return true;
      },
    }),
  );

  const definitionKeymap = Prec.highest(
    keymap.of([
      { key: "F12", run: (view) => lookup(view, "definition", view.state.selection.main.head, false) },
      { key: "Mod-F12", run: (view) => lookup(view, "implementation", view.state.selection.main.head, false) },
      { key: "Shift-F12", run: (view) => lookup(view, "references", view.state.selection.main.head, false) },
      { key: "Mod-Alt-F12", run: (view) => lookup(view, "definition", view.state.selection.main.head, true) },
      // These only ever claim the key while a picker or menu is open (each
      // returns false otherwise), so Enter still inserts a newline and the
      // arrow keys still move the caret the rest of the time.
      { key: "ArrowDown", run: moveTooltipSelection(1) },
      { key: "ArrowUp", run: moveTooltipSelection(-1) },
      { key: "Home", run: jumpTooltipSelection((state) => firstSelectableIndex(selectableRowCount(state), (i) => isRowSelectable(state, i))) },
      { key: "End", run: jumpTooltipSelection((state) => lastSelectableIndex(selectableRowCount(state), (i) => isRowSelectable(state, i))) },
      { key: "PageUp", run: pageTooltipSelection(-PICKER_PAGE_STEP) },
      { key: "PageDown", run: pageTooltipSelection(PICKER_PAGE_STEP) },
      { key: "Enter", run: commitTooltipSelection },
      { key: "Escape", run: dismissTooltip },
    ]),
  );

  return [baselineField, hoverField, codeIntelTooltipField, session, clickHandler, definitionKeymap];
}
