import { useEffect, useRef, useState } from "react";
import { copyText } from "../../../bridge/clipboard";
import { Copy, Check } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";

export function CellInspector() {
  const { inspectedCell, setInspectedCell } = useDatabaseStore(
    useShallow((s) => ({
      inspectedCell: s.inspectedCell,
      setInspectedCell: s.setInspectedCell,
    }))
  );
  const [copied, setCopied] = useState(false);
  const [format, setFormat] = useState<"auto" | "raw" | "json">("auto");

  // Keep the last value for Radix's short exit animation after closing.
  const previousCell = useRef(inspectedCell);
  useEffect(() => { if (inspectedCell) previousCell.current = inspectedCell; }, [inspectedCell]);
  const cell = inspectedCell ?? previousCell.current;
  if (!cell) return null;

  // SQL NULL has no text to show, copy or parse (#47).
  const isNull = cell.value === null;
  const rawValue = cell.value ?? "";
  let formattedJson: string | null = null;
  let isJson = false;

  try {
    const parsed = JSON.parse(rawValue);
    formattedJson = JSON.stringify(parsed, null, 2);
    isJson = true;
  } catch {
    isJson = false;
  }

  const displayText =
    format === "json" && isJson
      ? formattedJson!
      : format === "raw"
      ? rawValue
      : isJson
      ? formattedJson!
      : rawValue;

  function handleCopy() {
    void copyText(displayText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <Dialog open={Boolean(inspectedCell)} onOpenChange={(open) => { if (!open) setInspectedCell(null); }}>
      <DialogContent className="sm:max-w-2xl max-h-screen-85 flex flex-col gap-4 overflow-hidden [&>[data-slot=dialog-body]]:shrink [&>[data-slot=dialog-body]]:overflow-hidden">
        {/* Header */}
        <DialogHeader>
          <DialogTitle>Cell Inspector</DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-2">
            <span className="text-code-12 text-text-primary bg-state-soft px-1.5 py-0.5 rounded border border-border-subtle">
              {cell.columnName}
            </span>
            <span className="text-caption-11 text-text-tertiary">
              ({isNull ? "NULL" : `${rawValue.length} chars`} {isJson ? "· JSON" : ""})
            </span>
          </DialogDescription>
        </DialogHeader>
          <div className="flex flex-wrap items-center gap-1.5">
            {isJson && (
              <div className="flex items-center bg-surface-base border border-border rounded-md p-0.5 mr-2 text-caption-11">
                <button
                  type="button"
                  className={`px-2 py-0.5 rounded transition-control ${
                    format !== "raw" ? "bg-surface-raised text-text-primary font-medium" : "text-text-tertiary hover:text-text-primary"
                  }`}
                  onClick={() => setFormat("json")}
                >
                  Formatted
                </button>
                <button
                  type="button"
                  className={`px-2 py-0.5 rounded transition-control ${
                    format === "raw" ? "bg-surface-raised text-text-primary font-medium" : "text-text-tertiary hover:text-text-primary"
                  }`}
                  onClick={() => setFormat("raw")}
                >
                  Raw
                </button>
              </div>
            )}

            <Button
              variant="outline"
              size="sm"
              className="h-7 text-label-12 gap-1"
              onClick={handleCopy}
            >
              {copied ? <Check className="w-3.5 h-3.5 text-status-success" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copied ? "Copied" : "Copy"}</span>
            </Button>

          </div>

        {/* Content */}
        <div className="min-h-0 overflow-auto flex-1 rounded-md bg-surface-well p-3 text-code-12 text-text-primary select-text whitespace-pre-wrap break-all">
          {displayText}
        </div>

        {/* Footer */}
        <DialogFooter className="items-center sm:justify-between text-caption-11 text-text-tertiary">
          <span>Click outside or press Escape to close</span>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 text-label-12"
            onClick={() => setInspectedCell(null)}
          >
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
