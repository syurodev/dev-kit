import { useId, useRef, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { NativeSelect } from "@/components/ui/native-select";
import { Button } from "@/components/ui/button";
import { InlineFeedback } from "@/components/ui/feedback";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { TypedValueInput } from "./TypedValueInput";
import { validateCellValue } from "../sql/cellValue";
import type { CellValue } from "../sql/updateCell";

function Assignment({
  initialValue,
  type,
  nullable,
  onValue,
}: {
  initialValue: CellValue;
  type?: string;
  nullable: boolean;
  onValue: (value: CellValue) => void;
}) {
  const [value, setValue] = useState<CellValue>(initialValue);
  return (
    <TypedValueInput
      value={value}
      type={type}
      nullable={nullable}
      onChange={(next) => {
        setValue(next);
        onValue(next);
      }}
    />
  );
}
/** All assignments share one row selection, never all matching/unloaded rows. */
export function BulkEditDialog({
  rows,
  columns,
  columnTypes,
  columnNullable,
  keyColumns,
  engine,
  onStage,
  onClose,
  onRestoreFocus,
}: {
  rows: readonly number[];
  columns: readonly string[];
  columnTypes?: Readonly<Record<string, string>>;
  columnNullable?: Readonly<Record<string, boolean>>;
  keyColumns?: readonly string[];
  engine?: string;
  onStage: (values: readonly { column: number; value: CellValue }[]) => string;
  onClose: () => void;
  onRestoreFocus?: () => void;
}) {
  const [fields, setFields] = useState([
    {
      id: 0,
      column: Math.max(
        0,
        columns.findIndex((c) => !keyColumns?.includes(c)),
      ),
    },
  ]);
  const fieldPrefix = useId();
  const nextId = useRef(1);
  const values = useRef(new Map<number, CellValue>());
  const [error, setError] = useState("");
  const visible = useModuleVisibility();
  return (
    <Dialog
      open={visible}
      onOpenChange={(open) => {
        if (!open && visible) onClose();
      }}
    >
      <DialogContent
        onCloseAutoFocus={(e) => {
          if (onRestoreFocus) {
            e.preventDefault();
            if (visible && document.activeElement === document.body)
              onRestoreFocus();
          }
        }}
        className="max-h-[90dvh] overflow-y-auto sm:max-w-xl"
      >
        <DialogHeader>
          <DialogTitle>
            Update {rows.length} selected row{rows.length === 1 ? "" : "s"}
          </DialogTitle>
          <DialogDescription>
            Only rows on the loaded page are included. Choose a value for each
            column, then review the staged changes before writing.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            const assignments = fields.map((f) => ({
              column: f.column,
              value: values.current.has(f.id) ? values.current.get(f.id)! : "",
            }));
            for (const a of assignments) {
              const name = columns[a.column];
              const message = validateCellValue(
                a.value,
                columnTypes?.[name],
                columnNullable?.[name] !== false,
                engine,
              );
              if (message) {
                setError(`${name}: ${message}`);
                const index = assignments.indexOf(a);
                e.currentTarget
                  .querySelectorAll<HTMLElement>("[data-assignment]")
                  [index]?.querySelector<HTMLElement>(
                    "[data-value-editor] [id]:is(input,textarea,select):not(:disabled)",
                  )
                  ?.focus();
                return;
              }
            }
            const refusal = onStage(assignments);
            if (refusal) setError(refusal);
            else onClose();
          }}
        >
          {fields.map((f) => (
            <div
              key={f.id}
              data-assignment
              className="rounded-md border border-border-subtle p-3"
            >
              <div className="mb-2 flex items-end gap-2">
                <Field className="flex-1">
                  <FieldLabel htmlFor={`${fieldPrefix}-column-${f.id}`}>
                    Column
                  </FieldLabel>
                  <NativeSelect
                    id={`${fieldPrefix}-column-${f.id}`}
                    value={f.column}
                    onChange={(e) => {
                      values.current.delete(f.id);
                      setFields((all) =>
                        all.map((x) =>
                          x.id === f.id
                            ? { ...x, column: Number(e.target.value) }
                            : x,
                        ),
                      );
                      setError("");
                    }}
                  >
                    {columns.map((name, index) => (
                      <option
                        key={index}
                        value={index}
                        disabled={fields.some(
                          (x) => x.id !== f.id && x.column === index,
                        )}
                      >
                        {name}
                      </option>
                    ))}
                  </NativeSelect>
                </Field>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  disabled={fields.length === 1}
                  onClick={() => {
                    values.current.delete(f.id);
                    setFields((all) => all.filter((x) => x.id !== f.id));
                    setError("");
                  }}
                >
                  Remove
                </Button>
              </div>
              <div data-value-editor>
                <Assignment
                  key={f.column}
                  initialValue={
                    values.current.has(f.id) ? values.current.get(f.id)! : ""
                  }
                  type={columnTypes?.[columns[f.column]]}
                  nullable={columnNullable?.[columns[f.column]] !== false}
                  onValue={(value) => {
                    values.current.set(f.id, value);
                    if (error) setError("");
                  }}
                />
              </div>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="self-start"
            disabled={fields.length >= columns.length}
            onClick={() => {
              const unused = columns
                .map((_, index) => index)
                .filter((index) => !fields.some((f) => f.column === index));
              const column =
                unused.find((index) => !keyColumns?.includes(columns[index])) ??
                unused[0] ??
                -1;
              if (column >= 0)
                setFields((all) => [...all, { id: nextId.current++, column }]);
              setError("");
            }}
          >
            Add column
          </Button>
          {error ? <InlineFeedback>{error}</InlineFeedback> : null}
          <DialogFooter>
            <Button type="button" variant="ghost" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm">
              Stage {rows.length} row{rows.length === 1 ? "" : "s"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
