import { useState } from "react";
import { ChevronRight, KeyRound, Layers } from "lucide-react";
import {
  FramedCard,
  FramedCardHeader,
  FramedCardPreview,
  FramedCardRow,
} from "@/components/ui/framed-card";
import { cn } from "@/lib/utils";
import type { ForeignKey, ParsedSchema, SchemaColumn } from "./parse";

/** Rows a card lists before Show all — a schema can hold hundreds of tables. */
const LIST_CAP = 40;

const plural = (n: number, noun: string) => `${n} ${noun}${n === 1 ? "" : "s"}`;

function ShowAll({ shown, total, onClick }: { shown: number; total: number; onClick(): void }) {
  if (shown >= total) return null;
  return (
    <button
      type="button"
      onClick={onClick}
      className="px-(--space-inline) py-(--space-micro) text-left text-caption-11 text-text-tertiary transition-colors duration-(--motion-fast) hover:text-text-primary focus-visible:bg-(--state-soft) focus-visible:outline-none"
    >
      Show all {total}
    </button>
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
 * `schema.table` keys (tests, and a caller that knows what was asked about).
 */
export function SchemaResultCard({
  schema,
  heading,
  initiallyExpanded = [],
}: {
  schema: ParsedSchema;
  heading?: string;
  initiallyExpanded?: string[];
}) {
  const [expanded, setExpanded] = useState(() => new Set(initiallyExpanded));
  const [all, setAll] = useState(false);
  const total = schema.schemas.reduce((sum, s) => sum + s.tables.length, 0);
  const toggle = (key: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  let budget = all ? Number.POSITIVE_INFINITY : LIST_CAP;
  const shownSchemas = schema.schemas
    .map((s) => {
      const tables = s.tables.slice(0, Math.max(0, budget));
      budget -= tables.length;
      return { name: s.name, tables };
    })
    .filter((s, index) => s.tables.length > 0 || schema.schemas[index].tables.length === 0);
  const shown = shownSchemas.reduce((sum, s) => sum + s.tables.length, 0);

  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={Layers} title={heading ?? "Schema"} meta={plural(total, "table")} />
      <FramedCardPreview flush>
        {schema.schemas.length === 0 ? (
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
                    const key = `${s.name}.${table.name}`;
                    const open = expanded.has(key);
                    return (
                      <li key={key} className="flex min-w-0 flex-col">
                        <button
                          type="button"
                          aria-expanded={open}
                          onClick={() => toggle(key)}
                          className="flex w-full min-w-0 cursor-pointer items-center gap-(--space-inline) px-(--space-inline) py-(--space-micro) text-left transition-colors duration-(--motion-fast) hover:bg-(--state-soft) focus-visible:bg-(--state-soft) focus-visible:outline-none"
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
                            {table.columns.map((column) => (
                              <ColumnLine key={column.name} column={column} />
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
        <ShowAll shown={shown} total={total} onClick={() => setAll(true)} />
      </FramedCardPreview>
    </FramedCard>
  );
}

/**
 * `db.listForeignKeys` — the declared keys, one row each, read the way a
 * join is written: `table.column → referenced_table.referenced_column`. A key
 * into another schema names that schema underneath.
 */
export function ForeignKeysResultCard({ keys, heading }: { keys: ForeignKey[]; heading?: string }) {
  const [all, setAll] = useState(false);
  const shown = all ? keys : keys.slice(0, LIST_CAP);
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader icon={KeyRound} title={heading ?? "Foreign keys"} meta={plural(keys.length, "key")} />
      <FramedCardPreview flush>
        {keys.length === 0 ? (
          <div className="px-(--space-inline) py-(--space-micro) text-caption-11 text-text-tertiary">
            No foreign keys declared
          </div>
        ) : (
          shown.map((key) => (
            <FramedCardRow
              key={`${key.schema}.${key.table}.${key.column}->${key.referencedSchema}.${key.referencedTable}.${key.referencedColumn}`}
              label={
                <span className="text-code-12">
                  {`${key.table}.${key.column} → ${key.referencedTable}.${key.referencedColumn}`}
                </span>
              }
              detail={key.referencedSchema !== key.schema ? `${key.schema} → ${key.referencedSchema}` : undefined}
            />
          ))
        )}
        <ShowAll shown={shown.length} total={keys.length} onClick={() => setAll(true)} />
      </FramedCardPreview>
    </FramedCard>
  );
}
