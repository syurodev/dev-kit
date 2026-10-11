import { useEffect, type ReactNode } from "react";
import { CellInspector } from "./CellInspector";
import { useDatabaseStore } from "../databaseStore";

export function DatabaseDocumentLayout({
  active,
  children,
}: {
  active: boolean;
  children: ReactNode;
}) {
  const clear = useDatabaseStore((s) => s.setInspectedCell);
  useEffect(
    () => () => {
      if (active) clear(null);
    },
    [active, clear],
  );
  return (
    <div className="@container/db-document h-full min-h-0 min-w-0 overflow-hidden">
      <div className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden @2xl/db-document:flex-row">
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {children}
        </div>
        {active && <CellInspector />}
      </div>
    </div>
  );
}
