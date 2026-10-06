import type { QueryResult } from "@/bridge/database";
import type { CellValue } from "../sql/updateCell";

/** Copy the row array and only edited rows, not every cell in the page. */
export function stagedResult(
  result: QueryResult | null,
  staged: Readonly<Record<string, CellValue>>,
): QueryResult | null {
  if (!result || Object.keys(staged).length === 0) return result;
  const rows = result.rows.slice();
  const copied = new Set<number>();
  for (const [position, value] of Object.entries(staged)) {
    const parts = position.split(":");
    if (parts.length !== 2) continue;
    const row = Number(parts[0]),
      column = Number(parts[1]);
    if (
      !Number.isInteger(row) ||
      !Number.isInteger(column) ||
      row < 0 ||
      column < 0 ||
      !rows[row] ||
      column >= rows[row].length
    )
      continue;
    if (!copied.has(row)) {
      rows[row] = rows[row].slice();
      copied.add(row);
    }
    rows[row][column] = value;
  }
  return { ...result, rows };
}
