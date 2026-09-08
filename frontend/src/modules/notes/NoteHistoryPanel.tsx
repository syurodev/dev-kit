import { useCallback, useEffect, useState } from "react";
import { Clock, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import {
  getNoteVersion,
  listNoteVersions,
  restoreNoteVersion,
  type NoteVersion,
} from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";
import { lineDiff } from "./noteDiff";

/** "2h ago" — the panel answers how stale a version is, not when it was made. */
export function relativeAge(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "";
  const seconds = Math.max(0, Math.round((now - then) / 1000));
  if (seconds < 60) return "now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}

/** Figma history row (424:3583): the trigger is the only coloured word. */
function triggerClass(trigger: string): string {
  if (trigger === "pre-restore") return "text-status-recovery";
  return "text-text-tertiary";
}

const ROW = "flex h-8 w-full items-center gap-2 rounded-md border px-3 text-left";

/**
 * Figma `Notes / History Panel Content` (424:3578) and `History Diff`
 * (803:3680). A clock and "History" over the checkpoint rule, then one 32px
 * row per version — "v14" in Code/12 Medium, its age in Code/11, the trigger
 * at the right — with the picked row on a soft fill inside a focus stroke.
 *
 * Picking one diffs it against what is on screen now. Current is a row of
 * its own so the top of the list is always the thing you are comparing
 * against, not a version that happens to match it.
 */
export function NoteHistoryPanel({
  noteId,
  currentBody,
  onRestored,
}: {
  noteId: string;
  currentBody: string;
  onRestored: (body: string) => void;
}) {
  const [versions, setVersions] = useState<NoteVersion[]>([]);
  const [selected, setSelected] = useState<NoteVersion | null>(null);
  const [selectedBody, setSelectedBody] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      setVersions(await listNoteVersions(noteId));
      setError("");
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [noteId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function select(version: NoteVersion) {
    try {
      const snapshot = await getNoteVersion(version.id);
      setSelected(version);
      setSelectedBody(snapshot.body);
      setError("");
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function restore() {
    if (!selected) return;
    try {
      const note = await restoreNoteVersion(noteId, selected.id);
      onRestored(note.body);
      setSelected(null);
      await refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  const diff = selected ? lineDiff(selectedBody, currentBody) : [];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-1.5 px-4 pt-5 pb-2">
        <span className="flex items-center gap-2 text-body-14-medium text-text-primary">
          <Clock className="size-3.5 text-text-secondary" />
          History
        </span>
        <span className="text-code-11 text-text-tertiary">
          checkpoints every idle 5 min · keeps last 50
        </span>
      </div>

      {error && (
        <div className="mx-4 mb-2 rounded-md border border-(--border-danger) bg-(--feedback-danger-soft) px-3 py-2 text-body-12 text-status-danger">
          {error}
        </div>
      )}

      <ScrollArea className="min-h-0 flex-1">
        <ul className="flex flex-col gap-1.5 px-4 pb-4">
          <li>
            <button
              type="button"
              onClick={() => setSelected(null)}
              className={cn(
                ROW,
                selected ? "border-transparent hover:bg-state-soft" : "border-status-info bg-state-soft",
              )}
            >
              <span className="text-code-12-medium text-text-primary">v{versions.length + 1}</span>
              <span className="text-code-11 text-text-tertiary">now</span>
              <span className="ml-auto text-code-11 text-(--brand-text)">current</span>
            </button>
          </li>
          {versions.map((version, index) => (
            <li key={version.id}>
              <button
                type="button"
                onClick={() => void select(version)}
                className={cn(
                  ROW,
                  selected?.id === version.id
                    ? "border-status-info bg-state-soft"
                    : "border-transparent hover:bg-state-soft",
                )}
              >
                <span className="text-code-12-medium text-text-primary">v{versions.length - index}</span>
                <span className="text-code-11 text-text-tertiary">{relativeAge(version.createdAt)}</span>
                <span className={cn("ml-auto text-code-11", triggerClass(version.trigger))}>
                  {version.trigger}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </ScrollArea>

      {selected && (
        <div className="flex min-h-0 flex-[2] flex-col border-t border-border-subtle">
          <ScrollArea className="min-h-0 flex-1">
            <pre className="p-4 text-code-11">
              {diff.map((line, i) => (
                <div
                  key={i}
                  className={
                    line.kind === "added"
                      ? "bg-status-success/10 text-status-success"
                      : line.kind === "removed"
                        ? "bg-(--feedback-danger-soft) text-status-danger"
                        : "text-text-secondary"
                  }
                >
                  {line.kind === "added" ? "+ " : line.kind === "removed" ? "- " : "  "}
                  {line.text}
                </div>
              ))}
            </pre>
          </ScrollArea>
          <div className="border-t border-border-subtle px-4 py-3">
            <Button className="w-full" onClick={() => void restore()}>
              <RotateCcw />
              Restore this version
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
