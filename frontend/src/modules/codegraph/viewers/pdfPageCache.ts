import type { PdfPageImage } from "@/bridge/workspaceCode";

/**
 * Pages already drawn, by document revision and resolution. A reader scrolls
 * back over what they just passed; without this each return asked Go again and
 * showed the page blank first. Bounded by count — a page is about a megabyte as
 * a data URL — and a new revision never matches an old key.
 */
const LIMIT = 24;
const pages = new Map<string, PdfPageImage>();

export const pageKey = (folderId: string, path: string, revision: string, page: number, dpi: number) =>
  `${folderId}\u0000${path}\u0000${revision}\u0000${page}\u0000${dpi}`;

export function cachedPage(key: string): PdfPageImage | undefined {
  const hit = pages.get(key);
  if (hit) {
    pages.delete(key);
    pages.set(key, hit);
  }
  return hit;
}

export function rememberPage(key: string, image: PdfPageImage): void {
  pages.delete(key);
  pages.set(key, image);
  while (pages.size > LIMIT) pages.delete(pages.keys().next().value!);
}

export function forgetPages(): void {
  pages.clear();
}
