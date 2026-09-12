import { gutter, GutterMarker } from "@codemirror/view";
import type { EditorState } from "@codemirror/state";
import { statementMarkers, type StatementOutcome } from "./statementMarkers";
import type { SqlDialect } from "./statements";

/**
 * A tick or a cross beside the statement that ran.
 *
 * Twelve lines of CodeMirror over a pure function — which line gets which
 * mark is in statementMarkers, where it can be tested. This draws it.
 *
 * The spacer is not decoration: a gutter that appears the moment the first
 * statement finishes would shift every line of the script sideways while
 * you are reading it. It reserves the glyph's width from the start.
 */
class OutcomeMarker extends GutterMarker {
  constructor(private readonly outcome: StatementOutcome | "spacer") {
    super();
  }

  override toDOM() {
    const mark = document.createElement("span");
    mark.className = "cm-statement-mark";
    if (this.outcome === "spacer") {
      mark.textContent = "";
      return mark;
    }
    mark.dataset.outcome = this.outcome;
    // A glyph rather than an SVG: it inherits the editor's line height and
    // needs no sizing of its own, and the gutter is one character wide.
    mark.textContent = this.outcome === "ok" ? "✓" : "✕";
    mark.setAttribute("aria-hidden", "true");
    mark.title = this.outcome === "ok" ? "Ran without error" : "Failed";
    return mark;
  }
}

const SPACER = new OutcomeMarker("spacer");
const MARKERS = {
  ok: new OutcomeMarker("ok"),
  error: new OutcomeMarker("error"),
} as const;

export function statementStatusGutter(
  outcomes: Readonly<Record<string, StatementOutcome>>,
  dialect: SqlDialect,
) {
  // Recomputed when the document changes, not per line: lineMarker is called
  // once for every visible line, and splitting the script inside it would be
  // the split run forty times a frame.
  let cachedDoc: string | null = null;
  let cached = new Map<number, StatementOutcome>();
  const marksFor = (state: EditorState) => {
    const doc = state.doc.toString();
    if (doc !== cachedDoc) {
      cachedDoc = doc;
      cached = statementMarkers(doc, dialect, outcomes);
    }
    return cached;
  };

  return gutter({
    class: "cm-statement-gutter",
    lineMarker: (view, line) => {
      const outcome = marksFor(view.state).get(view.state.doc.lineAt(line.from).number);
      return outcome ? MARKERS[outcome] : null;
    },
    initialSpacer: () => SPACER,
  });
}
