import { Lock, LockOpen, X } from "lucide-react";
import type { KV } from "@/bridge/apiclient";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";

/**
 * The table every part of a request is edited in: params, headers, form
 * fields, path variables, an environment's variables.
 *
 * There is always one blank row at the bottom, and typing into it makes it a
 * row — no "Add" button to find first, which is the gesture Postman taught.
 * `fixedKeys` is for path variables, whose names come from the address: a
 * row per :segment, nothing to add or remove, only values to fill.
 */
export function KeyValueTable({
  rows,
  onChange,
  keyPlaceholder = "Key",
  valuePlaceholder = "Value",
  fixedKeys = false,
  toggleable = true,
  secrets,
  onToggleSecret,
  secretPlaceholder,
  emptyLabel,
}: {
  rows: readonly KV[];
  onChange: (rows: KV[]) => void;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  fixedKeys?: boolean;
  toggleable?: boolean;
  /** For a variables table: which rows are secret (by index). */
  secrets?: readonly boolean[];
  onToggleSecret?: (index: number) => void;
  /** What an untouched secret's empty value field says. */
  secretPlaceholder?: (index: number) => string | undefined;
  emptyLabel?: string;
}) {
  const shown: KV[] = fixedKeys ? [...rows] : [...rows, { key: "", value: "" }];

  function update(index: number, patch: Partial<KV>) {
    const next = shown.map((row, i) => (i === index ? { ...row, ...patch } : row));
    // The trailing blank row becomes a real row the moment it holds anything;
    // until then it is not part of the request.
    onChange(fixedKeys ? next : next.filter((row, i) => i < rows.length || row.key !== "" || row.value !== ""));
  }

  function remove(index: number) {
    onChange(rows.filter((_, i) => i !== index));
  }

  if (fixedKeys && rows.length === 0) {
    return emptyLabel ? <p className="px-1 py-2 text-caption-11 text-text-tertiary">{emptyLabel}</p> : null;
  }

  // Rows of fields, not a data table: every cell is an input a person typed
  // into, so the list is as long as what they wrote — a grid keeps the
  // columns aligned without the table semantics a screen reader would read
  // as data.
  const control = "var(--control-height-sm)";
  const gridTemplateColumns = [
    toggleable ? control : "",
    "minmax(0, 2fr)",
    "minmax(0, 3fr)",
    onToggleSecret ? control : "",
    fixedKeys ? "" : control,
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface-base text-code-12" role="group">
      <div className="grid items-center border-b border-border-subtle bg-surface-well/60 py-2 text-caption-11-medium text-text-tertiary" style={{ gridTemplateColumns }} aria-hidden="true">
        {toggleable ? <span /> : null}<span className="px-2">{keyPlaceholder}</span><span className="px-2">{valuePlaceholder}</span>{onToggleSecret ? <span /> : null}{fixedKeys ? null : <span />}
      </div>
      {shown.map((row, index) => {
        const blank = !fixedKeys && index === rows.length;
        const secret = secrets?.[index] ?? false;
        return (
          <div
            key={index}
            className={cn("group grid items-center border-b border-border-subtle transition-control last:border-b-0 hover:bg-state-soft focus-within:bg-surface-well", row.disabled && "opacity-50")}
            style={{ gridTemplateColumns }}
          >
            {toggleable ? (
              <div className="flex justify-center">
                {blank ? null : (
                  <Checkbox
                    checked={!row.disabled}
                    onCheckedChange={(checked) => update(index, { disabled: checked !== true })}
                    aria-label={row.disabled ? `Enable ${row.key}` : `Disable ${row.key}`}
                  />
                )}
              </div>
            ) : null}
            <input
              value={row.key}
              readOnly={fixedKeys}
              placeholder={blank ? keyPlaceholder : ""}
              spellCheck={false}
              aria-label={`${keyPlaceholder} ${index + 1}`}
              onChange={(event) => update(index, { key: event.target.value })}
              className={cn(
                "h-(--control-height-sm) w-full min-w-0 border-r border-border-subtle bg-transparent px-2 font-mono text-code-12 text-text-primary outline-none transition-control placeholder:text-text-tertiary focus:bg-surface-well focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info/40",
                fixedKeys && "text-text-secondary",
              )}
            />
            <input
              value={row.value}
              type={secret ? "password" : "text"}
              placeholder={secret ? (secretPlaceholder?.(index) ?? "") : blank ? valuePlaceholder : ""}
              spellCheck={false}
              autoComplete="off"
              aria-label={`${valuePlaceholder} for ${row.key || `row ${index + 1}`}`}
              onChange={(event) => update(index, { value: event.target.value })}
              className="h-(--control-height-sm) w-full min-w-0 bg-transparent px-2 font-mono text-code-12 text-text-primary outline-none transition-control placeholder:text-text-tertiary focus:bg-surface-well focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-status-info/40"
            />
            {onToggleSecret ? (
              <div className="flex justify-center">
                {blank ? null : (
                  <button
                    type="button"
                    onClick={() => onToggleSecret(index)}
                    aria-label={`${secret ? "Make plain" : "Make secret"}: ${row.key || `row ${index + 1}`}`}
                    aria-pressed={secret}
                    title={secret ? "Secret — sealed, never shown again. Click to make it plain." : "Plain — click to make it secret"}
                    className={cn("rounded p-1 transition-control hover:bg-surface-raised focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/40", secret ? "text-status-warning" : "text-text-tertiary")}
                  >
                    {secret ? <Lock className="size-(--icon-12)" /> : <LockOpen className="size-(--icon-12)" />}
                  </button>
                )}
              </div>
            ) : null}
            {fixedKeys ? null : (
              <div className="flex justify-center">
                {blank ? null : (
                  <button
                    type="button"
                    onClick={() => remove(index)}
                    aria-label={`Remove ${row.key || `row ${index + 1}`}`}
                    className="rounded p-1 text-text-tertiary opacity-0 transition-control group-hover:opacity-100 group-focus-within:opacity-100 hover:bg-surface-raised hover:text-text-primary focus:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/40"
                  >
                    <X className="size-(--icon-12)" />
                  </button>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
