import { FileText } from "lucide-react";
import { ToolResultRow } from "@/components/ui/tool-result-card";
import { dispatchIntent } from "@/workbench/intents";
import type { NoteResult } from "./noteResult";

/**
 * ToolResultView shared by notes.create and notes.update
 * (docs/dev-kit/tool-result-view.mdx) — title only, never the note's
 * decrypted body/preview (see noteResult.ts). The whole row opens it
 * through the existing `notes.reveal` intent.
 */
export function NoteResultCard({ note }: { note: NoteResult }) {
  return (
    <ToolResultRow
      icon={FileText}
      label={note.title || "Untitled"}
      onClick={() =>
        void dispatchIntent(
          "notes.reveal",
          { module: "notes", kind: "note", id: note.id, label: note.title },
          { placement: "beside" },
        )
      }
    />
  );
}
