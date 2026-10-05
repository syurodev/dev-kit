import type { QueryResult } from "@/bridge/database";

/** Standard database workload: 10k × 30, including long JSON/text cells. */
export function databasePerformanceFixture(): QueryResult {
  const columns = Array.from({ length: 30 }, (_, i) =>
    i === 0 ? "id" : `column_${i}`,
  );
  const rows = Array.from({ length: 10_000 }, (_, r) =>
    columns.map((_, c) =>
      c === 0
        ? String(r + 1)
        : c % 5 === 0
          ? JSON.stringify({
              row: r,
              column: c,
              message: "representative long text ".repeat(12),
            })
          : `value ${r}:${c}`,
    ),
  );
  return { columns, rows, rowCount: rows.length, truncated: false };
}
