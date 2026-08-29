import { useState } from "react";
import { Copy, Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDatabaseStore } from "../databaseStore";

export function CellInspector() {
  const { inspectedCell, setInspectedCell } = useDatabaseStore();
  const [copied, setCopied] = useState(false);
  const [format, setFormat] = useState<"auto" | "raw" | "json">("auto");

  if (!inspectedCell) return null;

  const rawValue = inspectedCell.value;
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
    navigator.clipboard.writeText(displayText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-2xl max-h-[85vh] flex flex-col overflow-hidden animate-in fade-in zoom-in-95 duration-150">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-surface-raised/50">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-text-primary">Cell Inspector</span>
            <span className="text-xs font-mono text-action-primary bg-action-primary/10 px-1.5 py-0.5 rounded border border-action-primary/20">
              {inspectedCell.columnName}
            </span>
            <span className="text-[11px] text-text-tertiary">
              ({rawValue.length} chars {isJson ? "· JSON" : ""})
            </span>
          </div>

          <div className="flex items-center gap-1.5">
            {isJson && (
              <div className="flex items-center bg-surface-base border border-border rounded-md p-0.5 mr-2 text-[11px]">
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
              className="h-7 text-xs gap-1"
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
        <div className="p-4 overflow-auto flex-1 font-mono text-xs text-text-primary bg-surface-base/80 select-text whitespace-pre-wrap break-all leading-relaxed">
          {displayText}
        </div>

        {/* Footer */}
        <div className="px-4 py-2 border-t border-border bg-surface-raised/30 flex items-center justify-between text-[11px] text-text-tertiary">
          <span>Click outside or press Escape to close</span>
          <Button
            variant="secondary"
            size="sm"
            className="h-7 text-xs"
            onClick={() => setInspectedCell(null)}
          >
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}
