import { useState } from "react";
import { Download, FileSpreadsheet, FileJson, X, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_CONSOLE, useDatabaseStore } from "../databaseStore";
import { exportQueryResult } from "../export";
import { errorMessage } from "../../../bridge/errors";

export function ExportModal() {
  // Not named `console`: shadowing the global inside a component is a trap.
  const modalConsoleId = useDatabaseStore((s) => s.modalConsoleId);
  const slice = useDatabaseStore((s) => s.consoles[modalConsoleId] ?? EMPTY_CONSOLE);
  const { showExportModal, setShowExportModal } = useDatabaseStore(
    useShallow((s) => ({
      showExportModal: s.showExportModal,
      setShowExportModal: s.setShowExportModal,
    }))
  );
  const [format, setFormat] = useState<"csv" | "json">("csv");
  const [filename, setFilename] = useState("query-result");
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState("");
  const [exportedPath, setExportedPath] = useState("");

  const queryResult = slice.queryResult;

  if (!showExportModal || !queryResult) return null;

  async function handleExport() {
    if (!queryResult) return;
    setExporting(true);
    setError("");
    setExportedPath("");
    try {
      const ext = format === "csv" ? ".csv" : ".json";
      const fullFilename = filename.endsWith(ext) ? filename : `${filename}${ext}`;
      const path = await exportQueryResult(queryResult, format, fullFilename);
      if (path) {
        setExportedPath(path);
      }
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setExporting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-md p-5 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-(--motion-base)">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Download className="w-4 h-4 text-action-primary" />
            <h3 className="text-title-14 text-text-primary">Export Query Results</h3>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-text-tertiary hover:text-text-primary"
            onClick={() => setShowExportModal(false)}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        <p className="text-body-12 text-text-secondary">
          Export the current {queryResult.rowCount} row(s) into a file using the native file saver.
        </p>

        {/* Format Selection Cards */}
        <div className="grid grid-cols-2 gap-3">
          <button
            type="button"
            className={`p-3 rounded-lg border text-left flex flex-col gap-1 transition-all ${
              format === "csv"
                ? "border-action-primary bg-action-primary/10 text-text-primary shadow-xs"
                : "border-border bg-surface-raised text-text-secondary hover:border-border/80"
            }`}
            onClick={() => setFormat("csv")}
          >
            <FileSpreadsheet className="w-5 h-5 text-action-primary" />
            <span className="text-title-13 mt-1">CSV Format</span>
            <span className="text-caption-11 text-text-tertiary">RFC 4180 standard table</span>
          </button>

          <button
            type="button"
            className={`p-3 rounded-lg border text-left flex flex-col gap-1 transition-all ${
              format === "json"
                ? "border-action-primary bg-action-primary/10 text-text-primary shadow-xs"
                : "border-border bg-surface-raised text-text-secondary hover:border-border/80"
            }`}
            onClick={() => setFormat("json")}
          >
            <FileJson className="w-5 h-5 text-action-primary" />
            <span className="text-title-13 mt-1">JSON Format</span>
            <span className="text-caption-11 text-text-tertiary">Array of string objects</span>
          </button>
        </div>

        {/* JSON String disclaimer per spec Screen 20 */}
        {format === "json" && (
          <div className="p-2.5 rounded bg-surface-raised border border-border text-caption-11 text-text-secondary">
            <strong>Note:</strong> JSON exports every column value strictly as a string (e.g. <code className="font-mono bg-surface-base px-1 py-0.5 rounded">{"\"id\": \"42\""}</code>) preserving the QueryResult wire schema without type guessing.
          </div>
        )}

        {/* Filename Input */}
        <div className="space-y-1.5">
          <label className="text-label-12 text-text-primary">Suggested Filename</label>
          <div className="flex items-center gap-1.5">
            <Input
              value={filename}
              onChange={(e) => setFilename(e.target.value)}
              className="text-label-12 h-8 flex-1"
            />
            <span className="text-code-12 text-text-tertiary">
              {format === "csv" ? ".csv" : ".json"}
            </span>
          </div>
        </div>

        {exportedPath && (
          <div className="p-2.5 rounded bg-status-success/10 border border-status-success/30 text-body-12 text-status-success flex items-center gap-2">
            <CheckCircle2 className="w-4 h-4 shrink-0" />
            <span className="truncate">Saved to {exportedPath}</span>
          </div>
        )}

        {error && (
          <div className="p-2.5 rounded bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger flex items-center gap-2">
            <AlertCircle className="w-4 h-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowExportModal(false)}
            disabled={exporting}
          >
            {exportedPath ? "Close" : "Cancel"}
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleExport}
            disabled={exporting || !filename.trim()}
          >
            {exporting ? "Saving…" : "Save File…"}
          </Button>
        </div>
      </div>
    </div>
  );
}
