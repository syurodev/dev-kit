import type { CellValue } from "../sql/updateCell";
/** Bound draft expansion and review SQL; these do not change backend query limits. */
export function bulkEditRefusal(
  rowCount: number,
  values: readonly CellValue[],
): string {
  if (rowCount * values.length > 10000)
    return "Select fewer rows or columns: a bulk edit supports up to 10,000 cells.";
  const bytes =
    values.reduce(
      (sum, v) =>
        sum + (v === null ? 0 : new TextEncoder().encode(v).byteLength),
      0,
    ) * rowCount;
  if (bytes > 1024 * 1024)
    return "Select fewer rows or use smaller values: a bulk edit supports up to 1 MiB of values.";
  return "";
}
