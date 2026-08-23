import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { getCodeGraphBlastRadius } from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";

/**
 * What a change to the open file can break.
 *
 * File-scoped by nature — the backend answers per file — which is why this
 * lives beside the file tree rather than in the node inspector. Answering a
 * file question while a method is selected would be answering the wrong one.
 *
 * Not computed on open: it walks a transitive closure, and most of the time you
 * are editing without asking. Press the button when you want to know.
 */
export function BlastRadiusPanel({
  filePath,
  onOpenFile,
}: {
  filePath: string;
  onOpenFile: (path: string) => void;
}) {
  const [files, setFiles] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // A result belongs to the file it was asked about; carrying it to the next
  // file would quietly attribute one file's dependents to another.
  useEffect(() => {
    setFiles(null);
    setError("");
  }, [filePath]);

  async function load() {
    setBusy(true);
    setError("");
    try {
      setFiles(await getCodeGraphBlastRadius(filePath));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="shrink-0 border-t border-border-subtle px-3 py-1.5">
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="secondary"
          className="h-6 shrink-0 px-2 text-xs"
          disabled={busy}
          onClick={() => void load()}
        >
          {busy ? "Checking…" : "Impact"}
        </Button>

        {/* An empty list and a failed call look identical if both render as
            nothing, so the no-dependents case says so in words. */}
        {files?.length === 0 && (
          <span className="text-[11px] text-text-tertiary">
            Nothing else depends on this file.
          </span>
        )}
        {files && files.length > 0 && (
          <span className="font-mono text-[11px] text-status-warning">
            {files.length} dependent file{files.length === 1 ? "" : "s"}
          </span>
        )}
        {error && <span className="truncate text-[11px] text-destructive">{error}</span>}
      </div>

      {files && files.length > 0 && (
        <ul className="mt-1 max-h-32 overflow-y-auto">
          {files.map((f) => (
            <li key={f}>
              <button
                type="button"
                onClick={() => onOpenFile(f)}
                className="w-full truncate rounded px-1 py-0.5 text-left font-mono text-[11px] text-text-secondary hover:bg-surface-raised"
              >
                {f}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
