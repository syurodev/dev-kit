import { useId, useState } from "react";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { NativeSelect } from "@/components/ui/native-select";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import { booleanValue, cellKind, splitOffset } from "../sql/cellValue";
import type { CellValue } from "../sql/updateCell";

/** Only the open editor renders controls; the grid still displays cheap text cells. */
export function TypedValueInput({
  value,
  onChange,
  type,
  nullable = true,
  error = "",
  autoFocus = false,
}: {
  value: CellValue;
  onChange: (value: CellValue) => void;
  type?: string;
  nullable?: boolean;
  error?: string;
  autoFocus?: boolean;
}) {
  const id = useId();
  const kind = cellKind(type);
  const temporal = ["date", "time", "datetime", "datetime-zone"].includes(kind);
  // WebKit's native temporal fields cannot represent sub-millisecond values,
  // durations or infinity. Keep their exact source in text instead of sanitising it.
  const needsText = Boolean(
    value &&
    temporal &&
    (/infinity/.test(value) ||
      /\.\d{4,}/.test(value) ||
      (kind === "time" &&
        (/^-/.test(value) || Number(value.split(":")[0]) > 23))),
  );
  const [raw, setRaw] = useState(needsText);
  const { local, offset } = splitOffset(value ?? "");
  const [localDraft, setLocalDraft] = useState(local);
  const [offsetDraft, setOffsetDraft] = useState(offset);
  const mode = value === null ? "null" : "value";
  return (
    <Field className="gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <FieldLabel htmlFor={id}>
          Value{" "}
          <span className="font-normal text-text-tertiary">
            {type || "text"}
          </span>
        </FieldLabel>
        <NativeSelect
          aria-label="Value or SQL NULL"
          value={mode}
          className="w-auto"
          onChange={(e) => {
            setLocalDraft("");
            setOffsetDraft("");
            onChange(e.target.value === "null" ? null : "");
          }}
        >
          <option value="value">Value</option>
          <option value="null" disabled={!nullable}>
            SQL NULL{nullable ? "" : " (not allowed)"}
          </option>
        </NativeSelect>
      </div>
      {kind === "boolean" ? (
        <NativeSelect
          id={id}
          value={value === null ? "" : booleanValue(value)}
          disabled={value === null}
          autoFocus={autoFocus}
          aria-invalid={Boolean(error)}
          aria-describedby={error ? `${id}-error` : undefined}
          onChange={(e) => onChange(e.target.value)}
        >
          <option value="" disabled>
            Choose value…
          </option>
          <option value="true">true</option>
          <option value="false">false</option>
        </NativeSelect>
      ) : kind === "json" ? (
        <Textarea
          id={id}
          value={value ?? ""}
          disabled={value === null}
          autoFocus={autoFocus}
          aria-invalid={Boolean(error)}
          aria-describedby={error ? `${id}-error` : undefined}
          className="h-32 resize-y font-mono"
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        <Input
          id={id}
          type={
            !raw && temporal
              ? kind === "date"
                ? "date"
                : kind === "time"
                  ? "time"
                  : "datetime-local"
              : "text"
          }
          step="any"
          value={
            value === null
              ? ""
              : !raw && temporal
                ? kind === "datetime-zone"
                  ? localDraft
                  : value.replace(" ", "T")
                : value
          }
          disabled={value === null}
          autoFocus={autoFocus}
          autoComplete="off"
          aria-invalid={Boolean(error)}
          aria-describedby={error ? `${id}-error` : undefined}
          onChange={(e) => {
            if (kind === "datetime-zone" && !raw) {
              setLocalDraft(e.target.value);
              onChange(`${e.target.value}${offsetDraft}`);
            } else {
              onChange(e.target.value);
              const parsed = splitOffset(e.target.value);
              setLocalDraft(parsed.local);
              setOffsetDraft(parsed.offset);
            }
          }}
          onFocus={(e) => {
            if (e.target.type === "text") e.target.select();
          }}
        />
      )}
      {kind === "datetime-zone" && !raw && value !== null ? (
        <Field>
          <FieldLabel htmlFor={`${id}-offset`}>UTC offset</FieldLabel>
          <Input
            id={`${id}-offset`}
            value={offsetDraft}
            placeholder="+07:00 or Z"
            autoComplete="off"
            onChange={(e) => {
              setOffsetDraft(e.target.value);
              onChange(`${localDraft}${e.target.value}`);
            }}
          />
          <FieldDescription>
            Keep an explicit offset; no conversion to your computer’s timezone.
          </FieldDescription>
        </Field>
      ) : null}
      {temporal ? (
        <div className="flex items-center justify-between gap-2">
          <FieldDescription>
            {needsText
              ? "Use text to preserve precision or special database values."
              : kind === "date"
                ? "YYYY-MM-DD"
                : kind === "time"
                  ? "HH:mm:ss; fractional seconds supported."
                  : "Date and time; fractional seconds supported."}
          </FieldDescription>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={raw && needsText}
            onClick={() => setRaw(!raw)}
          >
            {raw ? "Use picker" : "Use text"}
          </Button>
        </div>
      ) : kind === "time-zone" ? (
        <FieldDescription>
          Time with UTC offset, e.g. 14:30:00+07:00.
        </FieldDescription>
      ) : kind === "integer" || kind === "number" ? (
        <FieldDescription>Precision is kept exactly as typed.</FieldDescription>
      ) : kind === "text" ? (
        <FieldDescription>
          Empty text and SQL NULL are different values.
        </FieldDescription>
      ) : null}
      {error ? <FieldError id={`${id}-error`}>{error}</FieldError> : null}
    </Field>
  );
}
