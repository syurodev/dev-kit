import { useEffect, useState } from "react";
import { Download, FileSpreadsheet, FileJson, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { exportQueryResult } from "../export";
import { errorMessage } from "../../../bridge/errors";

/**
 * Writes a result out as CSV or JSON.
 *
 * A28: the result arrives on the store, not a console id. This modal only ever
 * needed a QueryResult, and reaching one through `consoles[modalConsoleId]`
 * meant only a console could export — a table tab, which has its rows right
 * there, had to re-run them through a console first. Whoever opens the modal
 * now hands over the rows and a name for them.
 */
export function ExportModal() {
  const { showExportModal, setShowExportModal, queryResult, suggestedFilename } = useDatabaseStore(
    useShallow((s) => ({
      showExportModal: s.showExportModal,
      setShowExportModal: s.setShowExportModal,
      queryResult: s.exportResult,
      suggestedFilename: s.exportFilename,
    }))
  );
  const [format, setFormat] = useState<"csv" | "json">("csv");
  const [filename, setFilename] = useState(suggestedFilename);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState("");
  const [exportedPath, setExportedPath] = useState("");

  // The modal is mounted for the life of the module and only hides itself, so
  // its fields outlive an export. Each opening starts from the name the caller
  // suggested, and without last time's outcome still sitting under it claiming
  // a file this export has not written.
  useEffect(() => {
    if (showExportModal) {
      setFilename(suggestedFilename);
      setError("");
      setExportedPath("");
    }
  }, [showExportModal, suggestedFilename]);

  if (!queryResult) return null;

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
    <Dialog open={showExportModal} onOpenChange={(open) => { if (!exporting) setShowExportModal(open); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"><Download className="size-4 text-text-secondary" />Export Query Results</DialogTitle>
          <DialogDescription>Export the {queryResult.rowCount} loaded row(s) as a file. From a table tab, this exports the page currently on screen.</DialogDescription>
        </DialogHeader>

        {/* Format Selection Cards */}
        <div className="grid grid-cols-2 gap-3">
          <button
            type="button"
            aria-pressed={format === "csv"}
            className={`relative p-3 rounded-lg border text-left flex flex-col gap-1 transition-control outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 ${
              format === "csv"
                ? "border-action-primary bg-action-primary/10 text-text-primary shadow-xs"
                : "border-border-subtle bg-surface-well text-text-secondary hover:bg-state-soft"
            }`}
            onClick={() => setFormat("csv")}
          >
            <FileSpreadsheet className="w-5 h-5 text-text-secondary" />
            {format === "csv" && <CheckCircle2 aria-hidden className="absolute right-3 top-3 size-3.5 text-text-primary" />}
            <span className="text-title-13 mt-1">CSV Format</span>
            <span className="text-caption-11 text-text-tertiary">RFC 4180 standard table</span>
          </button>

          <button
            type="button"
            aria-pressed={format === "json"}
            className={`relative p-3 rounded-lg border text-left flex flex-col gap-1 transition-control outline-none focus-visible:ring-2 focus-visible:ring-status-info/20 ${
              format === "json"
                ? "border-action-primary bg-action-primary/10 text-text-primary shadow-xs"
                : "border-border-subtle bg-surface-well text-text-secondary hover:bg-state-soft"
            }`}
            onClick={() => setFormat("json")}
          >
            <FileJson className="w-5 h-5 text-text-secondary" />
            {format === "json" && <CheckCircle2 aria-hidden className="absolute right-3 top-3 size-3.5 text-text-primary" />}
            <span className="text-title-13 mt-1">JSON Format</span>
            <span className="text-caption-11 text-text-tertiary">Array of string objects</span>
          </button>
        </div>

        {/* JSON String disclaimer per spec Screen 20 */}
        {format === "json" && (
          <div className="p-2.5 rounded bg-surface-raised border border-border text-caption-11 text-text-secondary">
            <strong>Note:</strong> JSON exports every column value strictly as a string (e.g. <code className="font-mono bg-surface-base px-1 py-0.5 rounded">{"\"id\": \"42\""}</code>) so exported values keep their original text.
          </div>
        )}

        {/* Filename Input */}
        <div className="space-y-1.5">
          <label htmlFor="export-filename" className="text-label-12 text-text-secondary">Suggested Filename</label>
          <div className="flex items-center gap-1.5">
            <Input
              id="export-filename"
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

        <DialogFooter>
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
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
