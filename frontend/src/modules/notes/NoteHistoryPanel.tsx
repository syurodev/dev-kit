import { useCallback, useEffect, useState } from "react";
import { History, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
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

/**
 * Figma `Notes / History Panel Content` and `Notes / History Diff Content`.
 *
 * The list is versions; picking one diffs it against what is on screen now.
 * Current is a row of its own so the top of the list is always the thing you
 * are comparing against, not a version that happens to match it.
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
      <div className="border-b border-border px-3 py-2">
        <div className="flex items-center gap-2">
          <History className="size-4 text-text-secondary" />
          <span className="text-title-14 text-text-primary">History</span>
        </div>
        <p className="mt-0.5 text-caption-11 text-text-tertiary">
          checkpoints every idle 5 min · keeps last 50
        </p>
      </div>

      {error && (
        <div className="border-b border-status-danger/30 bg-status-danger/10 px-3 py-2 text-body-12 text-status-danger">
          {error}
        </div>
      )}

      <ScrollArea className="min-h-0 flex-1">
        <ul className="p-2 text-body-12">
          <li>
            <button
              type="button"
              onClick={() => setSelected(null)}
              className={`flex w-full items-baseline gap-2 rounded px-2 py-1.5 text-left ${
                selected ? "hover:bg-surface-raised" : "bg-surface-raised text-text-primary"
              }`}
            >
              <span className="font-mono text-text-primary">current</span>
              <span className="text-text-tertiary">now</span>
            </button>
          </li>
          {versions.map((version, index) => (
            <li key={version.id}>
              <button
                type="button"
                onClick={() => void select(version)}
                className={`flex w-full items-baseline gap-2 rounded px-2 py-1.5 text-left ${
                  selected?.id === version.id
                    ? "bg-surface-raised text-text-primary"
                    : "hover:bg-surface-raised"
                }`}
              >
                <span className="font-mono text-text-primary">v{versions.length - index}</span>
                <span className="text-text-tertiary">{relativeAge(version.createdAt)}</span>
                <span className="ml-auto text-caption-11 text-text-tertiary">{version.trigger}</span>
              </button>
            </li>
          ))}
        </ul>
      </ScrollArea>

      {selected && (
        <div className="flex min-h-0 flex-[2] flex-col border-t border-border">
          <ScrollArea className="min-h-0 flex-1">
            <pre className="p-3 text-code-11">
              {diff.map((line, i) => (
                <div
                  key={i}
                  className={
                    line.kind === "added"
                      ? "bg-status-success/10 text-status-success"
                      : line.kind === "removed"
                        ? "bg-status-danger/10 text-status-danger"
                        : "text-text-secondary"
                  }
                >
                  {line.kind === "added" ? "+ " : line.kind === "removed" ? "- " : "  "}
                  {line.text}
                </div>
              ))}
            </pre>
          </ScrollArea>
          <div className="border-t border-border p-2">
            <Button size="sm" className="w-full" onClick={() => void restore()}>
              <RotateCcw />
              Restore this version
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
