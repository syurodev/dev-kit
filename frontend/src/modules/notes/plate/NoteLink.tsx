import { useQuery } from "@tanstack/react-query";
import { FileText, Link2 } from "lucide-react";
import { createPlatePlugin } from "platejs/react";
import { PlateElement, type PlateElementProps } from "platejs/react";
import type { TComboboxInputElement } from "platejs";
import {
  InlineCombobox,
  InlineComboboxContent,
  InlineComboboxEmpty,
  InlineComboboxGroup,
  InlineComboboxInput,
  InlineComboboxItem,
} from "@/components/ui/inline-combobox";
import { noteLinkMarkup, resolveNoteLinks, searchNotes } from "../../../bridge/notes";

export const NOTE_LINK_KEY = "note_link";
export const NOTE_LINK_INPUT_KEY = "note_link_input";

/**
 * A link to another note, stored as an id and rendered as that note's title
 * now. Nothing in the body holds the label — renaming a note updates every
 * link to it without rewriting a single body.
 *
 * This is an inline void element, which the editor had none of: LinkKit is
 * inline but wraps real text, and ImageUpload is void but block-level. So the
 * plugin registers inline + void on the element, and Slate handles cursor
 * traversal past it as an atomic unit.
 */
export const NoteLinkPlugin = createPlatePlugin({
  key: NOTE_LINK_KEY,
  node: { isElement: true, isInline: true, isVoid: true },
});

export function NoteLinkElement(props: PlateElementProps) {
  const noteId = (props.element as { noteId?: string }).noteId ?? "";

  const { data: target } = useQuery({
    queryKey: ["notes", "link", noteId],
    queryFn: async () => {
      if (!noteId) return null;
      const [t] = await resolveNoteLinks([noteId]);
      return t ?? null;
    },
    enabled: Boolean(noteId),
  });

  const label = target?.title ?? "";
  const state: "live" | "deleted" | "missing" = target
    ? target.missing
      ? "missing"
      : target.deleted
        ? "deleted"
        : "live"
    : "live";

  return (
    <PlateElement {...props} as="span">
      <span
        contentEditable={false}
        data-note-link={noteId}
        title={state === "deleted" ? "This note is in the trash" : undefined}
        className={`inline-flex items-center gap-1 rounded-sm border px-1.75 py-0.5 align-baseline text-label-12 ${
          state === "live"
            ? "border-status-info bg-status-info/8 text-status-info"
            : state === "deleted"
              ? "border-glass-border bg-state-soft text-text-tertiary line-through opacity-75"
              : "border-dashed border-glass-border bg-state-soft text-text-tertiary line-through"
        }`}
      >
        <Link2 className="size-2.75" />
        {label || (state === "missing" ? "deleted note" : "…")}
      </span>
      {props.children}
    </PlateElement>
  );
}

/** The `[[` combobox — the same InlineCombobox the slash menu uses, on a
 *  different trigger. Suggestions come from NotesSearch, which already
 *  matches on title. */
export function NoteLinkInputElement(props: PlateElementProps<TComboboxInputElement>) {
  const { editor, element } = props;

  const { data: results = [] } = useQuery({
    queryKey: ["notes", "search", ""],
    queryFn: () => searchNotes(""),
  });

  return (
    <PlateElement {...props} as="span">
      <InlineCombobox element={element} trigger="[[">
        <InlineComboboxInput />
        <InlineComboboxContent>
          <InlineComboboxEmpty>No notes</InlineComboboxEmpty>
          <InlineComboboxGroup>
            {results.map((note) => (
              <InlineComboboxItem
                key={note.id}
                value={note.id}
                label={note.title || "Untitled"}
                keywords={[note.title]}
                onClick={() => {
                  // Written as its markdown form. Bodies serialize to markdown,
                  // so the node has to survive that round trip as text.
                  editor.tf.insertText(noteLinkMarkup(note.id));
                }}
              >
                <FileText className="size-3.5" />
                <span className="flex-1 truncate">{note.title || "Untitled"}</span>
              </InlineComboboxItem>
            ))}
          </InlineComboboxGroup>
        </InlineComboboxContent>
      </InlineCombobox>
      {props.children}
    </PlateElement>
  );
}
