import { Decoration, MatchDecorator, ViewPlugin, WidgetType, type DecorationSet, type EditorView, type ViewUpdate } from "@codemirror/view";

/**
 * Where a wrapped line of code may break, beyond the spaces a browser already
 * allows.
 *
 * Two alternatives, told apart by which group matched:
 *   1. BEFORE a member access (`.name`, `::name`) — where a person breaks a
 *      call chain by hand. The lookahead keeps `1.5` and `...` whole.
 *   2. AFTER an opening bracket, a comma or a path slash, when more of the
 *      token follows. Before whitespace the browser can already break, so a
 *      decoration there would be pure cost.
 *
 * Language-agnostic on purpose: these are punctuation conventions shared by
 * every language this editor is likely to show, not grammar.
 */
export const BREAK_OPPORTUNITY = /(\.(?=[A-Za-z_$])|::(?=[A-Za-z_$]))|([(\[,/](?=[^\s)\]]))/g;

/** Offsets into `text` where a break opportunity belongs. */
export function breakOffsets(text: string): number[] {
  const offsets: number[] = [];
  for (const match of text.matchAll(BREAK_OPPORTUNITY)) {
    offsets.push(match[1] !== undefined ? match.index : match.index + match[0].length);
  }
  return offsets;
}

class BreakWidget extends WidgetType {
  eq() {
    return true;
  }

  toDOM() {
    return document.createElement("wbr");
  }

  ignoreEvent() {
    return false;
  }
}

const breakWidget = Decoration.widget({ widget: new BreakWidget(), side: 1 });

const decorator = new MatchDecorator({
  regexp: BREAK_OPPORTUNITY,
  decorate: (add, from, to, match) => {
    const pos = match[1] !== undefined ? from : to;
    add(pos, pos, breakWidget);
  },
});

/**
 * Lets a wrapped line break at punctuation instead of mid-identifier.
 *
 * To a browser, `@RequestHeader(SyncHeaderResolver.DEVICE_HEADER)` is one word:
 * there is no space in it, so there is nowhere to break. CodeMirror's
 * `overflow-wrap: anywhere` then does two unpleasant things. It first moves the
 * whole word to the next row — leaving a row that holds nothing but the line's
 * indentation — and only then, because the word still does not fit, cuts it at
 * whatever character happens to reach the edge (`.D│EVICE_HEADER`).
 *
 * A <wbr> at each punctuation boundary gives the browser somewhere better, and
 * because the word can now be split it no longer has to leave the first row.
 *
 * Only meaningful alongside EditorView.lineWrapping, and only added with it.
 */
export const wrapBreaks = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet;

    constructor(view: EditorView) {
      this.decorations = decorator.createDeco(view);
    }

    update(update: ViewUpdate) {
      this.decorations = decorator.updateDeco(update, this.decorations);
    }
  },
  { decorations: (v) => v.decorations },
);
