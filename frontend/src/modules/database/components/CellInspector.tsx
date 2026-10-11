import { useEffect, useState } from "react";
import { Copy, Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { useDatabaseStore } from "../databaseStore";

/** A non-modal inspector keeps the selected data beside the value being read. */
export function CellInspector() {
  const cell = useDatabaseStore((s) => s.inspectedCell);
  const close = useDatabaseStore((s) => s.setInspectedCell);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const [format, setFormat] = useState("formatted");
  useEffect(() => {
    setCopied(false);
    setError("");
  }, [cell]);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);
  if (!cell) return null;
  const raw = cell.value ?? "";
  let pretty: string | null = null;
  try {
    pretty = JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    /* Plain text. */
  }
  const text = format === "formatted" && pretty !== null ? pretty : raw;
  return (
    <section
      aria-label="Cell inspector"
      className="flex max-h-[45%] min-h-0 w-full shrink-0 flex-col border-t border-border-subtle bg-surface-base @2xl/db-document:max-h-none @2xl/db-document:w-80 @2xl/db-document:border-t-0 @2xl/db-document:border-l"
    >
      <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle px-3 py-2">
        <span
          className="flex-1 truncate text-body-13 font-medium"
          title={cell.columnName}
        >
          {cell.columnName}
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Close cell inspector"
          onClick={() => close(null)}
        >
          <X />
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-2 px-3 py-2">
        <span className="mr-auto text-code-11 text-text-tertiary">
          {cell.value === null ? "SQL NULL" : `${raw.length} chars`}
        </span>
        {pretty !== null && (
          <NativeSelect
            size="sm"
            aria-label="Inspector format"
            value={format}
            onChange={(e) => setFormat(e.target.value)}
          >
            <NativeSelectOption value="formatted">
              Formatted JSON
            </NativeSelectOption>
            <NativeSelectOption value="raw">Raw</NativeSelectOption>
          </NativeSelect>
        )}
        <Button
          variant="ghost"
          size="xs"
          disabled={cell.value === null}
          onClick={async () => {
            try {
              await copyText(text);
              setCopied(true);
              setError("");
            } catch (e) {
              setError(errorMessage(e));
            }
          }}
        >
          {copied ? (
            <Check className="size-3.5" />
          ) : (
            <Copy className="size-3.5" />
          )}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      {error && (
        <p role="alert" className="px-3 text-body-12 text-status-danger">
          {error}
        </p>
      )}
      <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all p-3 text-code-12 text-text-primary select-text">
        {cell.value === null ? "NULL" : text}
      </pre>
    </section>
  );
}
