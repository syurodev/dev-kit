import { useEffect, useId, useState } from "react";
import { GitBranch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { getCodeGraphBlastRadius } from "@/bridge/codegraph";
import { errorMessage } from "@/bridge/errors";
import { useCodeGraphStore } from "./codegraphStore";

type Props = { filePath: string; onOpenFile: (path: string) => void };

/** On-demand file impact; the portal leaves all of the editor's height available. */
export function BlastRadiusPanel({ filePath, onOpenFile }: Props) {
  const [open, setOpen] = useState(false);
  const titleId = useId();
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const indexedAt = useCodeGraphStore((s) => s.indexInfo?.indexedAt);
  const indexing = useCodeGraphStore((s) => s.busy || Boolean(s.indexInfo?.indexing));

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
            key={JSON.stringify([projectPath, filePath, indexedAt, indexing])}
            filePath={filePath}
            projectPath={projectPath}
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
function ImpactResult({ filePath, projectPath, indexedAt, indexing, onOpenFile }: Props & {
  projectPath: string;
  indexedAt: number | undefined;
  indexing: boolean;
}) {
  const [files, setFiles] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [visibleCount, setVisibleCount] = useState(100);
  useEffect(() => {
    if (indexing || !projectPath) return;
    let active = true;
    const current = () => {
      const state = useCodeGraphStore.getState();
      return active && state.projectPath === projectPath && state.indexInfo?.indexedAt === indexedAt
        && !state.busy && !state.indexInfo?.indexing;
    };
    void getCodeGraphBlastRadius(filePath).then(
      (result) => { if (current()) setFiles(result); },
      (reason) => { if (current()) setError(errorMessage(reason)); },
    );
    return () => { active = false; };
  }, [filePath, projectPath, indexedAt, indexing, attempt]);

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
    <p role="status" className="text-body-12 text-text-tertiary">Nothing else depends on this file.</p>
  );
  return (
    <>
      <p role="status" className="text-body-12 text-text-secondary tabular-nums">
        {files.length} dependent file{files.length === 1 ? "" : "s"}
      </p>
      <ul className="max-h-64 overflow-y-auto scrollbar-gutter-stable">
        {files.slice(0, visibleCount).map((path) => (
          <li key={path}>
            <button
              type="button"
              title={path}
              onClick={() => onOpenFile(path)}
              className="h-7 w-full truncate rounded px-2 text-left text-code-11 text-text-secondary transition-colors duration-(--motion-fast) hover:bg-surface-raised focus-visible:bg-surface-raised focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-status-info"
            >
              {path}
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
