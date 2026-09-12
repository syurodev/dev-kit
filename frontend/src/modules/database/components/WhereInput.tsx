import { useMemo, useRef } from "react";
import {
  acceptCompletion,
  completionStatus,
  startCompletion,
  type CompletionContext,
  type CompletionResult,
} from "@codemirror/autocomplete";
import {
  MariaSQL,
  MySQL,
  PostgreSQL,
  schemaCompletionSource,
  type SQLNamespace,
} from "@codemirror/lang-sql";
import { LanguageSupport } from "@codemirror/language";
import { EditorState, Prec, type Extension } from "@codemirror/state";
import { EditorView, keymap, placeholder, tooltips } from "@codemirror/view";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { sqlCompletionTheme } from "./sqlCompletionTheme";
import type { SchemaNamespace } from "@/bridge/database";

/**
 * What the WHERE box completes against.
 *
 * The offline index is the source — it already holds every schema, table and
 * column of the connection, which is what makes `orders.customer_id` complete
 * in a filter on `invoices`. This table's own columns are layered on top of it
 * because the result in the grid is authoritative and free: they are correct
 * the moment the first page lands, indexed or not, so a connection nobody has
 * indexed still completes the one table you are actually looking at.
 */
export function whereSchema(
  indexed: SchemaNamespace | undefined,
  schema: string,
  table: string,
  columns: readonly string[],
): SQLNamespace {
  if (columns.length === 0) {
    return (indexed ?? {}) as SQLNamespace;
  }
  const namespace: Record<string, unknown> = { ...(indexed ?? {}) };
  const existing = (namespace[schema] ?? {}) as Record<string, unknown>;
  namespace[schema] = { ...existing, [table]: [...columns] };
  return namespace as SQLNamespace;
}

/**
 * The words a WHERE clause is written in.
 *
 * Curated, rather than the dialect's own list. That list is every reserved and
 * non-reserved word the engine has — several hundred — so typing "nam" in a
 * filter offered NAME, NAMES, NAMESPACE, RENAME and DYNAMIC above the
 * `username` column two rows down. None of them can appear in a WHERE clause.
 * Sinking them with a negative boost was not enough: an exact prefix match on
 * a keyword still outranks a mid-word match on a column, and it should — the
 * fix is for the keyword not to be offered at all.
 *
 * SELECT/FROM/WHERE are here because a filter may contain a subquery:
 * `id IN (SELECT employee_id FROM his_shifts)`.
 */
export const WHERE_KEYWORDS = [
  "AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "ILIKE", "BETWEEN", "EXISTS",
  "ANY", "ALL", "TRUE", "FALSE", "CASE", "WHEN", "THEN", "ELSE", "END", "CAST",
  "AS", "INTERVAL", "SELECT", "FROM", "WHERE", "DISTINCT", "CURRENT_DATE",
  "CURRENT_TIMESTAMP",
];

const whereKeywordSource = (context: CompletionContext): CompletionResult | null => {
  const word = context.matchBefore(/\w+/);
  if (!word || (word.from === word.to && !context.explicit)) {
    return null;
  }
  return {
    from: word.from,
    options: WHERE_KEYWORDS.map((label) => ({
      label,
      type: "keyword",
      // Below anything the database actually has, so a column and a keyword
      // that match equally well put the column first.
      boost: -20,
    })),
    validFor: /^\w*$/,
  };
};

function dialectFor(engine: string | undefined) {
  switch ((engine ?? "postgres").toLowerCase()) {
    case "mysql":
      return MySQL;
    case "mariadb":
      return MariaSQL;
    default:
      return PostgreSQL;
  }
}

/**
 * The filter box, as a one-line SQL editor.
 *
 * A `<input>` cannot complete a column name, and the console two tabs away has
 * completed them since it was written — same dialect, same offline index, same
 * `@codemirror/lang-sql`. This is that editor with the chrome of a text field:
 * `defaultTable` is what makes a bare `branch_id` complete here, where the
 * console would want `his_attendances.branch_id`.
 */
export function WhereInput({
  value,
  onChange,
  onApply,
  engine,
  indexed,
  schema,
  table,
  columns,
}: {
  value: string;
  onChange: (value: string) => void;
  onApply: () => void;
  engine: string | undefined;
  /** The offline index's namespace, when the connection has been indexed. */
  indexed: SchemaNamespace | undefined;
  schema: string;
  table: string;
  /** This table's columns, from the page in the grid. */
  columns: readonly string[];
}) {
  // onApply is a new function on every parent render. Depending on it would
  // reconfigure the whole editor — language, keymap and all — on each one.
  const onApplyRef = useRef(onApply);
  onApplyRef.current = onApply;

  const extensions = useMemo<Extension[]>(
    () => [
      // Assembled rather than `sql(config)`, which bundles the dialect's whole
      // keyword list as a second completion source with no way to turn it off.
      // Same language and highlighting, two sources of our own.
      new LanguageSupport(dialectFor(engine).language, [
        dialectFor(engine).language.data.of({
          autocomplete: schemaCompletionSource({
            schema: whereSchema(indexed, schema, table, columns),
            defaultSchema: schema,
            // The whole point: a filter on one table is written in ITS column
            // names, unqualified. Without this every suggestion arrives as
            // `public.his_attendances.branch_id`.
            defaultTable: table,
          }),
        }),
        dialectFor(engine).language.data.of({ autocomplete: whereKeywordSource }),
      ]),
      // One line, enforced at the transaction: a WHERE clause with a newline in
      // it is a box that has silently grown a second row under the toolbar.
      // Pasted newlines become spaces rather than being dropped, so pasting a
      // wrapped clause still gives valid SQL.
      EditorState.transactionFilter.of((tr) => {
        if (!tr.docChanged) {
          return tr;
        }
        return tr.newDoc.lines > 1
          ? [{ changes: { from: 0, to: tr.startState.doc.length, insert: tr.newDoc.toString().replace(/\s*\n\s*/g, " ") } }]
          : tr;
      }),
      placeholder("status = 'open'"),
      sqlCompletionTheme,
      // The completion popup hangs off document.body. Left inside the editor
      // it is a child of a one-line box with overflow clipped both ways, so
      // every suggestion was drawn inside the input and cut in half.
      tooltips({ parent: document.body, position: "fixed" }),
      Prec.highest(
        keymap.of([
          {
            key: "Enter",
            run: (view) => {
              // While the popup is open Enter belongs to it. Running the query
              // instead would make every completion unreachable by keyboard.
              if (completionStatus(view.state) === "active") {
                return acceptCompletion(view);
              }
              onApplyRef.current();
              return true;
            },
          },
          { key: "Mod-Space", run: startCompletion },
        ]),
      ),
      // A text field, not an editor: no gutter, no active-line tint, one row
      // high, and the caller's border around it.
      EditorView.theme({
        "&": { height: "100%", backgroundColor: "transparent" },
        "&.cm-focused": { outline: "none" },
        ".cm-gutters": { display: "none" },
        ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "transparent" },
        ".cm-scroller": { overflowX: "auto", overflowY: "hidden" },
        ".cm-content": { padding: "0", display: "flex", alignItems: "center" },
        ".cm-line": { padding: "0" },
      }),
    ],
    [engine, indexed, schema, table, columns],
  );

  return (
    // Undrawn at rest. A border here makes the filter read as a widget bolted
    // onto the toolbar rather than as the line of query it is — and the row it
    // sits in is already separated from the strip above by a rule. The tint on
    // hover and the border on focus are what say it takes typing.
    <div className="flex h-(--control-height-sm) min-w-0 flex-1 items-center rounded-md border border-transparent bg-transparent px-2 text-code-12 hover:bg-surface-well focus-within:border-status-info focus-within:bg-surface-well">
      <CodeEditor value={value} onChange={onChange} extensions={extensions} />
    </div>
  );
}
