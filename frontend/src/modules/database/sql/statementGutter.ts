import { GutterMarker, lineNumberMarkers } from "@codemirror/view";
import { RangeSet, StateField, type EditorState } from "@codemirror/state";
import { statementMarkers, type StatementOutcome } from "./statementMarkers";
import type { SqlDialect } from "./statements";

/**
 * How a statement went, in the colour of its line number.
 *
 * It was a tick and a cross in a gutter of their own, and the gutter was a
 * second column of chrome for one character of information. The line number
 * is already there, already aligned with the statement, and already the
 * thing the eye uses to find a line — so it carries the answer instead and
 * the console gets its width back.
 *
 * Colour is not the only channel: a failure is also bold. Red and green are
 * the pair that deuteranopia collapses, and "which of these five statements
 * failed" is exactly the question a colour-blind reader would be left unable
 * to answer. Weight survives any palette and any eye.
 *
 * Which line gets which mark is in statementMarkers, where it is pure and
 * tested. This is the CodeMirror end: a field that rebuilds the set when the
 * document changes, because every offset moves when a line is inserted above.
 */
class OutcomeMarker extends GutterMarker {
  constructor(outcome: StatementOutcome) {
    super();
    this.elementClass = `cm-statement-${outcome}`;
  }
}

const MARKERS = {
  ok: new OutcomeMarker("ok"),
  error: new OutcomeMarker("error"),
} as const;

export function statementStatusGutter(
  outcomes: Readonly<Record<string, StatementOutcome>>,
  dialect: SqlDialect,
) {
  const build = (state: EditorState) => {
    const marks = statementMarkers(state.doc.toString(), dialect, outcomes);
    if (marks.size === 0) return RangeSet.empty;
    return RangeSet.of(
      [...marks].map(([line, outcome]) => MARKERS[outcome].range(state.doc.line(line).from)),
      // The map is built in document order already; sorting is cheap
      // insurance against that changing and is what RangeSet.of expects.
      true,
    );
  };

  return StateField.define<RangeSet<GutterMarker>>({
    create: build,
    update: (current, transaction) => (transaction.docChanged ? build(transaction.state) : current),
    provide: (field) => lineNumberMarkers.from(field),
  });
}
