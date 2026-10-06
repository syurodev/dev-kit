export type LibraryEntry = {
  id: string;
  sql: string;
  name?: string;
  createdAt?: string;
};
/** Only bounded plain text appears in rows; full SQL is copied/opened explicitly. */
export function libraryPreview(sql: string): string {
  return (
    sql.slice(0, 300).replace(/\s+/g, " ").trim() +
    (sql.length > 300 ? "…" : "")
  );
}
export function filterLibrary<T extends LibraryEntry>(
  entries: readonly T[],
  search: string,
): T[] {
  const needle = search.trim().toLowerCase();
  return entries.filter(
    (e) =>
      !needle ||
      e.name?.toLowerCase().includes(needle) ||
      e.sql.toLowerCase().includes(needle),
  );
}
