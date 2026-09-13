import { useState } from "react";
import { copyText } from "../../../bridge/clipboard";
import { Copy, Check, X } from "lucide-react";
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

  if (!inspectedCell) return null;

  // SQL NULL has no text to show, copy or parse (#47).
  const isNull = inspectedCell.value === null;
  const rawValue = inspectedCell.value ?? "";
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-2xl max-h-[85vh] flex flex-col overflow-hidden animate-in fade-in zoom-in-95 duration-(--motion-base)">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-surface-raised/50">
          <div className="flex items-center gap-2">
            <span className="text-title-13 text-text-primary">Cell Inspector</span>
            <span className="text-code-12 text-action-primary bg-action-primary/10 px-1.5 py-0.5 rounded border border-action-primary/20">
              {inspectedCell.columnName}
            </span>
            <span className="text-caption-11 text-text-tertiary">
              ({isNull ? "NULL" : `${rawValue.length} chars`} {isJson ? "· JSON" : ""})
            </span>
          </div>

          <div className="flex items-center gap-1.5">
            {isJson && (
              <div className="flex items-center bg-surface-base border border-border rounded-md p-0.5 mr-2 text-caption-11">
                <button
                  type="button"
                  className={`px-2 py-0.5 rounded transition-colors ${
                    format !== "raw" ? "bg-surface-raised text-text-primary font-medium" : "text-text-tertiary hover:text-text-primary"
                  }`}
                  onClick={() => setFormat("json")}
                >
                  Formatted
                </button>
                <button
                  type="button"
                  className={`px-2 py-0.5 rounded transition-colors ${
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

            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-text-tertiary hover:text-text-primary"
              onClick={() => setInspectedCell(null)}
            >
              <X className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {/* Content */}
        <div className="p-4 overflow-auto flex-1 text-code-12 text-text-primary bg-surface-base/80 select-text whitespace-pre-wrap break-all">
          {displayText}
        </div>

        {/* Footer */}
        <div className="px-4 py-2 border-t border-border bg-surface-raised/30 flex items-center justify-between text-caption-11 text-text-tertiary">
          <span>Click outside or press Escape to close</span>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 text-label-12"
            onClick={() => setInspectedCell(null)}
          >
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}
