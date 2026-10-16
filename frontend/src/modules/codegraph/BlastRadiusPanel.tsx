import { FileIcon } from "@/components/file-icon/FileIcon";
import { useEffect, useId, useState } from "react";
import { GitBranch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { errorMessage } from "@/bridge/errors";
import { useCodeGraphStore } from "./codegraphStore";
import { blastRadiusOf } from "./codegraphActions";
import { isUnsupportedProjectError } from "./codegraphErrors";
import { NOT_SUPPORTED } from "./sidebar/sidebarModel";

type Props = { folderId: string; filePath: string; onOpenFile: (path: string) => void };

/**
 * On-demand file impact, from the index of the folder the file is in: the files
 * of THAT folder that depend on it. Dependencies across folders are not known to
 * the index and are not shown; the panel says whose index it read.
 */
export function BlastRadiusPanel({ folderId, filePath, onOpenFile }: Props) {
  const [open, setOpen] = useState(false);
  const titleId = useId();
  const indexedAt = useCodeGraphStore((s) => s.data[folderId]?.entries?.indexedAt);
  const indexing = useCodeGraphStore((s) => {
    const d = s.data[folderId];
    return d?.indexStatus?.state === "indexing" || d?.indexStatus?.state === "pending" || d?.entries?.state === "indexing";
  });

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>
            <Button size="icon-sm" variant="ghost" aria-label="Show file impact">
              <GitBranch className="size-3.5" aria-hidden="true" />
            </Button>
          </PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>File impact</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" aria-labelledby={titleId} className="w-96 max-w-screen-gutter">
        <h2 id={titleId} className="text-body-13-medium">File impact</h2>
        {open && (
          <ImpactResult
            key={JSON.stringify([folderId, filePath, indexedAt, indexing])}
            folderId={folderId}
            filePath={filePath}
            indexedAt={indexedAt}
            indexing={indexing}
            onOpenFile={(path) => {
              setOpen(false);
              onOpenFile(path);
            }}
          />
        )}
      </PopoverContent>
    </Popover>
  );
}

// Keyed to the index as well as the file: a re-index cannot leave an old
// dependency list visible, and a delayed bridge response cannot overwrite it.
function ImpactResult({ folderId, filePath, indexedAt, indexing, onOpenFile }: Props & {
  indexedAt: number | undefined;
  indexing: boolean;
}) {
  const [files, setFiles] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const [unsupported, setUnsupported] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [visibleCount, setVisibleCount] = useState(100);
  useEffect(() => {
    if (indexing) return;
    let active = true;
    const current = () => active && useCodeGraphStore.getState().data[folderId]?.entries?.indexedAt === indexedAt;
    void blastRadiusOf(folderId, filePath).then(
      (result) => { if (current()) setFiles(result); },
      (reason) => {
        if (!current()) return;
        if (isUnsupportedProjectError(reason)) setUnsupported(true);
        else setError(errorMessage(reason));
      },
    );
    return () => { active = false; };
  }, [folderId, filePath, indexedAt, indexing, attempt]);

  // A fact about the folder, not a failure: no retry to offer.
  if (unsupported) return (
    <p role="status" className="text-body-12 text-text-tertiary">
      {NOT_SUPPORTED} — no analyzer reads this folder, so what depends on this file is unknown.
    </p>
  );
  if (error) return (
    <div role="alert" className="flex flex-col items-start gap-2">
      <p className="break-words text-body-12 text-status-danger">{error}</p>
      <Button size="xs" variant="outline" onClick={() => { setError(""); setAttempt((n) => n + 1); }}>
        Try again
      </Button>
    </div>
  );
  if (!files) return (
    <p role="status" className="text-body-12 text-text-tertiary">
      {indexing ? "Waiting for the index…" : "Checking dependencies…"}
    </p>
  );
  if (!files.length) return (
    <p role="status" className="text-body-12 text-text-tertiary">Nothing else in this folder depends on this file.</p>
  );
  return (
    <>
      <p role="status" className="text-body-12 text-text-secondary tabular-nums">
        {files.length} dependent file{files.length === 1 ? "" : "s"} in this folder
      </p>
      <ul className="max-h-64 overflow-y-auto scrollbar-gutter-stable">
        {files.slice(0, visibleCount).map((path) => (
          <li key={path}>
            <button
              type="button"
              title={path}
              onClick={() => onOpenFile(path)}
              className="flex h-7 w-full min-w-0 items-center gap-1.5 rounded px-2 text-left text-code-11 text-text-secondary transition-colors duration-(--motion-fast) hover:bg-surface-raised focus-visible:bg-surface-raised focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-status-info"
            >
              <FileIcon path={path} className="size-3.5" /><span className="truncate">{path}</span>
            </button>
          </li>
        ))}
      </ul>
      {visibleCount < files.length && (
        <Button size="xs" variant="ghost" onClick={() => setVisibleCount((n) => n + 100)}>
          Show more ({files.length - visibleCount} remaining)
        </Button>
      )}
    </>
  );
}
