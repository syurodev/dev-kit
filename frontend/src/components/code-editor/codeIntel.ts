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
import { detectPlatform } from "@/lib/platform";

/**
 * A place code can be defined. `label`/`kind` are optional because a
 * provider may only know a path and a line (e.g. a grep-backed fallback) —
 * the picker still has to render something for that location.
 */
export type CodeLocation = { path: string; line: number; label?: string; kind?: string };

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
 * underline, no F12 binding doing anything). Shaped like LSP on purpose —
 * `textDocument/definition` takes a document position and returns locations
 * — so a future LSP-backed provider is a thin adapter, not a redesign.
 */
export interface CodeIntelProvider {
  definition?(request: CodeIntelRequest, signal: AbortSignal): Promise<CodeLocation[]>;
}

export type CodeIntelConfig = {
  path: string;
  provider: CodeIntelProvider;
  onNavigate: (location: CodeLocation, options: { beside: boolean }) => void;
};

/**
 * The identifier touching `column` in `lineText`, or null when the position
 * sits inside nothing word-like.
 *
 * `\p{L}\p{N}_$`, not a syntax tree: this editor shows Java, Go, Python,
 * SQL and more, and a token pattern shared by all of them beats a grammar
 * for any one. Excluding a leading digit is what keeps `123` a number
 * rather than a half-identifier — every mainstream language's own
 * identifier grammar draws the line there, so `1.5` and `0x1F` still read
 * as numbers, not identifiers that happen to start oddly.
 *
 * Matches are treated as touching `column` when `column` sits anywhere from
 * their first character up to and including their last — i.e. the caret
 * slot right after the identifier still counts, the same way clicking just
 * past a word still selects it in most editors. That is what lets a click
 * right before the `.` in `foo.bar` resolve to `foo` instead of finding
 * nothing.
 */
export function identifierAt(lineText: string, column: number): { from: number; to: number; text: string } | null {
  const IDENTIFIER = /[\p{L}_$][\p{L}\p{N}_$]*/gu;
  for (const match of lineText.matchAll(IDENTIFIER)) {
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

/**
 * What the go-to-definition tooltip is showing, if anything. `onNavigate` is
 * carried on the picker's own state rather than closed over by the tooltip's
 * `create` function, because that function (below) is a single module-level
 * value shared by every editor with codeIntel attached — it has to read
 * per-editor things like `onNavigate` out of the state it is handed, not out
 * of a closure that would only ever be one editor's.
 */
type CodeIntelTooltipState =
  | { kind: "message"; pos: number; text: string }
  | {
      kind: "picker";
      pos: number;
      items: CodeLocation[];
      selected: number;
      beside: boolean;
      onNavigate: CodeIntelConfig["onNavigate"];
    };

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
  return { pos: state.pos, above: true, create: createCodeIntelTooltipView };
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

    const list = document.createElement("ul");
    list.className = "cm-code-intel-picker";
    list.setAttribute("role", "listbox");
    list.setAttribute("aria-label", "Definitions");
    state.items.forEach((location, index) => {
      const option = document.createElement("li");
      option.className = "cm-code-intel-option";
      option.setAttribute("role", "option");
      option.setAttribute("aria-selected", index === state.selected ? "true" : "false");
      if (index === state.selected) option.classList.add("cm-code-intel-option-selected");

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
      path.textContent = `${location.path}:${location.line}`;
      option.appendChild(path);

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
    dom.appendChild(list);
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

function moveTooltipSelection(delta: number) {
  return (view: EditorView): boolean => {
    const state = view.state.field(codeIntelTooltipField, false);
    if (!state || state.kind !== "picker") return false;
    const count = state.items.length;
    const selected = (state.selected + delta + count) % count;
    view.dispatch({ effects: setCodeIntelTooltip.of({ ...state, selected }) });
    return true;
  };
}

function commitTooltipSelection(view: EditorView): boolean {
  const state = view.state.field(codeIntelTooltipField, false);
  if (!state || state.kind !== "picker") return false;
  const location = state.items[state.selected];
  view.dispatch({ effects: setCodeIntelTooltip.of(null) });
  state.onNavigate(location, { beside: state.beside });
  return true;
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

/**
 * Headless go-to-definition. Knows nothing about where answers come from —
 * only `config.provider` does — and adds no UI for a method the provider
 * doesn't implement.
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

      async goToDefinition(pos: number, beside: boolean) {
        const definition = config.provider.definition;
        if (!definition) return;
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
          locations = await definition(request, controller.signal);
        } catch {
          failed = true;
        }

        if (this.destroyed || seq !== this.requestSeq) return;

        if (locations.length === 1) {
          config.onNavigate(locations[0], { beside });
          return;
        }
        // The document may have been edited while the lookup was out, and a
        // tooltip anchored past its end throws. Clamping rather than mapping:
        // an edit also closes the tooltip (see codeIntelTooltipField), so a
        // slightly-off anchor is on screen for one frame at most.
        const tooltipPos = Math.min(anchor, view.state.doc.length);
        view.dispatch({
          effects: setCodeIntelTooltip.of(
            locations.length === 0
              ? {
                  kind: "message",
                  pos: tooltipPos,
                  text: failed ? "Could not look up definition" : "No definition found",
                }
              : {
                  kind: "picker",
                  pos: tooltipPos,
                  items: locations,
                  selected: 0,
                  beside,
                  onNavigate: config.onNavigate,
                },
          ),
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

  function goToDefinition(view: EditorView, pos: number, beside: boolean): boolean {
    const current = view.plugin(session);
    if (!current || !config.provider.definition) return false;
    void current.goToDefinition(pos, beside);
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
        return goToDefinition(view, pos, event.altKey);
      },
    }),
  );

  const definitionKeymap = Prec.highest(
    keymap.of([
      { key: "F12", run: (view) => goToDefinition(view, view.state.selection.main.head, false) },
      { key: "Mod-F12", run: (view) => goToDefinition(view, view.state.selection.main.head, true) },
      // These four only ever claim the key when the picker is open (each
      // returns false otherwise), so Enter still inserts a newline and the
      // arrow keys still move the caret the rest of the time.
      { key: "ArrowDown", run: moveTooltipSelection(1) },
      { key: "ArrowUp", run: moveTooltipSelection(-1) },
      { key: "Enter", run: commitTooltipSelection },
      { key: "Escape", run: dismissTooltip },
    ]),
  );

  return [baselineField, hoverField, codeIntelTooltipField, session, clickHandler, definitionKeymap];
}
