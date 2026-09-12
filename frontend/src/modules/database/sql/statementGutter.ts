import {
  Decoration,
  EditorView,
  GutterMarker,
  WidgetType,
  lineNumberMarkers,
  type DecorationSet,
} from "@codemirror/view";
import { RangeSet, StateField, type EditorState } from "@codemirror/state";
import { statementMarkers, type StatementOutcome } from "./statementMarkers";
import { formatDuration } from "../consoleStatus";
import type { StatementRun } from "../databaseStore";
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

/**
 * How long the statement took, at the end of its line.
 *
 * A console with five statements in it raises one question a pill cannot
 * answer: which of them is the slow one. The number sits where the eye
 * already is — past the end of the statement, in the tertiary colour, out
 * of the way of the SQL.
 */
class TimingWidget extends WidgetType {
  constructor(private readonly label: string) {
    super();
  }

  // Without this every redraw replaces every widget, and a widget replaced
  // mid-selection takes the selection with it.
  override eq(other: TimingWidget) {
    return other.label === this.label;
  }

  override toDOM() {
    const timing = document.createElement("span");
    timing.className = "cm-statement-timing";
    timing.textContent = this.label;
    // It is not part of the statement. A screen reader reading the line
    // should read the SQL, and a copy of the line should paste as SQL.
    timing.setAttribute("aria-hidden", "true");
    return timing;
  }

  override ignoreEvent() {
    return true;
  }
}

type Marks = { lines: RangeSet<GutterMarker>; timings: DecorationSet };

export function statementStatusGutter(
  runs: Readonly<Record<string, StatementRun>>,
  dialect: SqlDialect,
) {
  const build = (state: EditorState): Marks => {
    const marks = statementMarkers(state.doc.toString(), dialect, runs);
    if (marks.size === 0) {
      return { lines: RangeSet.empty, timings: Decoration.none };
    }
    const entries = [...marks];
    return {
      lines: RangeSet.of(
        entries.map(([line, run]) => MARKERS[run.outcome].range(state.doc.line(line).from)),
        // The map is built in document order already; sorting is cheap
        // insurance against that changing and is what RangeSet.of expects.
        true,
      ),
      timings: Decoration.set(
        entries.map(([line, run]) => {
          const end = state.doc.line(line).to;
          return Decoration.widget({
            widget: new TimingWidget(formatDuration(run.durationMs)),
            // After the line's own text, and after anything else that
            // claims the same position.
            side: 1,
          }).range(end);
        }),
        true,
      ),
    };
  };

  return StateField.define<Marks>({
    create: build,
    update: (current, transaction) => (transaction.docChanged ? build(transaction.state) : current),
    provide: (field) => [
      lineNumberMarkers.from(field, (marks) => marks.lines),
      EditorView.decorations.from(field, (marks) => marks.timings),
    ],
  });
}
