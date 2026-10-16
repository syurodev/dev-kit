/**
 * How a file is shown when opened as a Files document. Text is the editor;
 * Markdown is the editor with a rendered view to switch to; a PDF is a viewer
 * and has no text buffer at all (Go draws its pages, see PdfDocument).
 *
 * Decided by the name alone, so opening a file never has to read it first —
 * and a `.pdf` that is not a PDF fails in the viewer, with the reason.
 */
export type ViewerKind = "text" | "markdown" | "pdf";

const MARKDOWN = /\.(md|markdown|mdown|mkd)$/i;
const PDF = /\.pdf$/i;

export function viewerKindOf(path: string): ViewerKind {
  if (PDF.test(path)) return "pdf";
  if (MARKDOWN.test(path)) return "markdown";
  return "text";
}

/** A file with no text buffer: reading it as text would only fail. */
export const isPdfPath = (path: string) => viewerKindOf(path) === "pdf";
