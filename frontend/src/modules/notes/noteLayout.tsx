import { useLayoutEffect, useRef } from "react";
import { cn } from "@/lib/utils";
import type { NoteFont } from "@/stores/noteView";

/**
 * Where a note's text sits in its pane. Shared by NoteEditor and the gallery
 * (`?gallery#notes-editor-layout`) so what is checked there is what ships.
 *
 * The writing column is 720px (Figma `Editor Content`, 424:3473) unless the
 * note is full width (noteView.ts). The editor's left padding is the
 * centring inset, and every top-level block except a table keeps its right
 * edge on the column (`margin-right`: what is left of the editor past 720px).
 * A table takes the pane's whole width, centred like the text: a table of
 * sentences inside 720px was a strip of two-word lines that scrolled sideways
 * while the rest of the pane sat empty, and one that spread only to the right
 * pushed the whole page off centre.
 *
 * Not `mx-auto` per block: Plate's indent (every list item, and Tab) writes
 * `margin-left` inline on the block, which beat the auto margin and threw
 * bullets to the pane's left edge. Padding on the editor leaves a block's
 * left margin to the indent, measured from the column.
 *
 * The Editor's own default variant is not used: its gutter (64px a side, room
 * for drag handles Notes does not have) sat inside the column and left 544px
 * to write in.
 */
export type NoteView = {
  fullWidth: boolean;
  smallText: boolean;
  font: NoteFont;
  /** Read-only: the editor draws no table controls then. */
  locked: boolean;
};

/**
 * A typeface choice swaps `--font-sans` for the page, and every type utility
 * (`text-body-16`, `text-display-32`, …) reads that variable — so the title,
 * the body and the headings follow, while code (`--font-mono`) stays code.
 */
export const NOTE_FONT_CLASS: Record<NoteFont, string> = {
  default: "",
  serif: "[--font-sans:ui-serif,Georgia,Cambria,serif]",
  mono: "[--font-sans:var(--font-mono)]",
};

export function notePageClass(font: NoteFont) {
  return cn("w-full px-6 pt-5 pb-24", NOTE_FONT_CLASS[font]);
}

/** The breadcrumb row spans the pane whatever the column does, like Notion's top bar. */
export const NOTE_CRUMB_CLASS = "flex h-4 w-full items-center gap-2 text-code-11 text-text-tertiary";

function noteColumnClass(isFullWidth: boolean) {
  return cn("mx-auto w-full", !isFullWidth && "max-w-180");
}

export function noteTitleClass(isFullWidth: boolean) {
  return cn("mt-5 block", noteColumnClass(isFullWidth));
}

/** For `<Editor variant="none">`. */
export function noteBodyClass(view: NoteView) {
  return cn(
    // overflow-x-visible: the table's 8px controls column sits left of the
    // editor's box once the table reaches the pane's edge.
    "mt-4 size-full overflow-x-visible pt-4 pb-72",
    view.smallText ? "text-body-14" : "text-body-16",
    !view.fullWidth && [
      "pl-note-inset",
      "[&>*:not(.slate-table)]:mr-note-overhang",
      // A table takes the pane's whole width, the same on both sides, so the
      // page stays centred: pulled left by the inset (which, measured against
      // the editor's content box, is the overhang), plus the 8px the table's
      // controls column hangs into the gutter — a locked note draws none.
      view.locked ? "[&>.slate-table]:-ml-note-overhang" : "[&>.slate-table]:-ml-note-bleed",
    ],
  );
}

/**
 * The note's title wraps instead of scrolling sideways: a single-line input cut
 * a long title (agents write them) at the column's edge. Enter moves into the
 * body rather than adding a line, and pasted line breaks become spaces — a
 * title is one line of text that happens to wrap.
 */
export function NoteTitleField({
  value,
  className,
  readOnly,
  onChange,
  onEnter,
}: {
  value: string;
  className?: string;
  readOnly?: boolean;
  onChange: (next: string) => void;
  onEnter: () => void;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);

  // Height follows the text; re-fit when the column's width changes too,
  // since that moves where the lines break.
  useLayoutEffect(() => {
    const field = ref.current;
    if (!field) return;
    const fit = () => {
      field.style.height = "auto";
      field.style.height = `${field.scrollHeight}px`;
    };
    fit();
    if (typeof ResizeObserver === "undefined") return;
    // Only a change of width moves line breaks; the field's own height
    // changes (and the body growing below it) are not a reason to measure.
    let width = field.clientWidth;
    const observer = new ResizeObserver(() => {
      if (field.clientWidth === width) return;
      width = field.clientWidth;
      fit();
    });
    observer.observe(field);
    return () => observer.disconnect();
  }, [value]);

  return (
    <textarea
      ref={ref}
      aria-label="note-title"
      rows={1}
      value={value}
      readOnly={readOnly}
      onChange={(e) => onChange(e.target.value.replace(/\r?\n/g, " "))}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.nativeEvent.isComposing) {
          e.preventDefault();
          onEnter();
        }
      }}
      className={cn(
        "w-full resize-none overflow-hidden border-none bg-transparent px-0 py-1 text-display-32 text-text-primary outline-none",
        className,
      )}
    />
  );
}
