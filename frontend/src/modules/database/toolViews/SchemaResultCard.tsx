import { useState } from "react";
import { ChevronRight, KeyRound, Layers } from "lucide-react";
import { FramedCard, FramedCardHeader, FramedCardPreview, FramedCardRow } from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import { listWindow, tableKey, visibleSchemas } from "./lists";
import type { ForeignKey, ParsedSchema, SchemaColumn } from "./parse";

const plural = (n: number, noun: string) => `${n} ${noun}${n === 1 ? "" : "s"}`;

/** The ring every control here draws on focus, inset so the preview's clip cannot cut it. */
const FOCUS = "focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset focus-visible:outline-none";

/** The toggle under a capped list, and what even the opened list leaves out. */
function ListFooter({ toggle, notShown, onToggle }: { toggle: string | null; notShown: number; onToggle(): void }) {
  return (
    <>
      {notShown > 0 ? (
        <div className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
          {`+${notShown} more not shown`}
        </div>
      ) : null}
      {toggle ? (
        <button
          type="button"
          onClick={onToggle}
          className={cn(
            "rounded-sm px-(--space-inline) py-(--space-micro) text-left text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary",
            FOCUS,
          )}
        >
          {toggle}
        </button>
      ) : null}
    </>
  );
}

function ColumnLine({ column }: { column: SchemaColumn }) {
  return (
    // 7 = 28px: the row's 8px inset, the 12px chevron and the gap after it — lined up with the table's name.
    <li className="flex min-w-0 items-baseline gap-(--space-inline) py-0.5 pr-(--space-inline) pl-7">
      <span className="min-w-0 truncate text-code-12 text-text-primary">{column.name}</span>
      {column.dataType ? (
        <span className="min-w-0 truncate text-caption-11 text-text-tertiary">{column.dataType}</span>
      ) : null}
      {column.nullable === null ? null : (
        <span className="ml-auto shrink-0 text-caption-11 text-text-tertiary">
          {column.nullable ? "nullable" : "not null"}
        </span>
      )}
    </li>
  );
}

/**
 * `db.schema.get` — what the schema index holds: schemas, their tables with
 * a column count, and a table's columns on demand. `initiallyExpanded` takes
 * lists.ts `tableKey`s and `initiallyAll` starts the list opened (tests, and
 * a caller that knows what was asked about).
 */
export function SchemaResultCard({
  schema,
  heading,
  initiallyExpanded = [],
  initiallyAll = false,
}: {
  schema: ParsedSchema;
  heading?: string;
  initiallyExpanded?: string[];
  initiallyAll?: boolean;
}) {
  const [expanded, setExpanded] = useState(() => new Set(initiallyExpanded));
  const [all, setAll] = useState(initiallyAll);
  const total = schema.schemas.reduce((sum, s) => sum + s.tables.length, 0);
  const view = listWindow(total, all);
  const shownSchemas = visibleSchemas(schema.schemas, view.shown);
  const toggle = (key: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Layers} title={heading ?? "Schema"} meta={plural(total, "table")} />
      <FramedCardPreview flush>
        {total === 0 ? (
          <div className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            No tables indexed
          </div>
        ) : (
          shownSchemas.map((s) => (
            <section key={s.name} aria-label={s.name} className="flex min-w-0 flex-col">
              <div className="truncate px-(--space-inline) pt-(--space-inline) pb-(--space-micro) text-label-11-caps text-text-tertiary">
                {s.name}
              </div>
              {s.tables.length === 0 ? (
                <div className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
                  No tables
                </div>
              ) : (
                <ul className="flex min-w-0 flex-col">
                  {s.tables.map((table) => {
                    const key = tableKey(s.name, table.name);
                    const open = expanded.has(key);
                    return (
                      <li key={key} className="flex min-w-0 flex-col">
                        <button
                          type="button"
                          aria-expanded={open}
                          onClick={() => toggle(key)}
                          className={cn(
                            "flex w-full min-w-0 cursor-pointer items-center gap-(--space-inline) px-(--space-inline) py-(--space-micro) text-left transition-colors duration-(--motion-fast) hover:bg-(--state-soft)",
                            FOCUS,
                          )}
                        >
                          <ChevronRight
                            aria-hidden="true"
                            className={cn(
                              "size-(--icon-12) shrink-0 text-icon-secondary transition-transform duration-(--motion-fast)",
                              open && "rotate-90",
                            )}
                          />
                          <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{table.name}</span>
                          <span className="shrink-0 text-caption-11 text-text-tertiary tabular-nums">
                            {plural(table.columns.length, "column")}
                          </span>
                        </button>
                        {open ? (
                          <ul className="flex min-w-0 flex-col pb-(--space-micro)">
                            {table.columns.map((column, index) => (
                              <ColumnLine key={`${index}\u0000${column.name}`} column={column} />
                            ))}
                          </ul>
                        ) : null}
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          ))
        )}
        <ListFooter toggle={view.toggle} notShown={view.notShown} onToggle={() => setAll((open) => !open)} />
      </FramedCardPreview>
    </FramedCard>
  );
}

/**
 * `db.listForeignKeys` — the declared keys, one row each, read the way a
 * join is written: `table.column → referenced_table.referenced_column`. A key
 * into another schema names both schemas underneath.
 */
export function ForeignKeysResultCard({
  keys,
  heading,
  initiallyAll = false,
}: {
  keys: ForeignKey[];
  heading?: string;
  initiallyAll?: boolean;
}) {
  const [all, setAll] = useState(initiallyAll);
  const view = listWindow(keys.length, all);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={KeyRound} title={heading ?? "Foreign keys"} meta={plural(keys.length, "key")} />
      <FramedCardPreview flush>
        {keys.length === 0 ? (
          <div className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            No foreign keys declared
          </div>
        ) : (
          keys.slice(0, view.shown).map((key, index) => (
            <FramedCardRow
              key={[index, key.schema, key.table, key.column, key.referencedSchema, key.referencedTable, key.referencedColumn].join("\u0000")}
              label={
                <span className="text-code-12">
                  {`${key.table}.${key.column} → ${key.referencedTable}.${key.referencedColumn}`}
                </span>
              }
              detail={key.referencedSchema !== key.schema ? `${key.schema} → ${key.referencedSchema}` : undefined}
            />
          ))
        )}
        <ListFooter toggle={view.toggle} notShown={view.notShown} onToggle={() => setAll((open) => !open)} />
      </FramedCardPreview>
    </FramedCard>
  );
}
