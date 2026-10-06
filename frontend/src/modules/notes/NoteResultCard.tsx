import { FileText } from "lucide-react";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { dispatchIntent } from "@/workbench/intents";
import type { NoteResult } from "./noteResult";

/**
 * `notes.create` / `notes.update` — the note's title only. Not its preview or
 * body: that is decrypted vault data, and less of it on screen than the raw
 * JSON used to show is the point.
 */
export function NoteResultCard({ note, heading }: { note: NoteResult; heading?: string }) {
  return (
    <FramedCard data-testid="tool-result-card">
      <FramedCardHeader
        icon={FileText}
        title={heading ?? "Note"}
        action={
          <FramedCardAction
            onClick={() =>
              void dispatchIntent(
                "notes.reveal",
                { module: "notes", kind: "note", id: note.id, label: note.title },
                { placement: "beside" },
              )
            }
          >
            Open
          </FramedCardAction>
        }
      />
      <FramedCardPreview>
        <p className="min-w-0 truncate text-body-13 text-text-primary">{note.title || "Untitled"}</p>
      </FramedCardPreview>
    </FramedCard>
  );
}
