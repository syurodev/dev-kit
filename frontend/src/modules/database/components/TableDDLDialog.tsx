import { useEffect, useMemo, useState } from "react";
import { Check, Copy } from "lucide-react";
import { sql as codemirrorSql, MariaSQL, MySQL, PostgreSQL } from "@codemirror/lang-sql";
import { useShallow } from "zustand/react/shallow";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { tableDDL } from "@/bridge/database";
import { useDatabaseStore } from "../databaseStore";

/**
 * A table's definition, as the engine itself reports it.
 *
 * A dialog rather than a tab: DDL is read and copied, not kept. It is also
 * deliberately NOT opened in a console — a console is a thing you press Run
 * on, and the last statement anyone wants to run by accident is the one that
 * creates the table they are looking at.
 *
 * Read-only, and not reformatted. On MySQL it is the server's own SHOW CREATE
 * TABLE, down to the engine clause and the collation; on Postgres it is rebuilt
 * from the catalogues with the server printing every expression. Either way it
 * ends up pasted into a migration, which is why nothing here rewrites it.
 */
export function TableDDLDialog() {
  const { target, close, profileId, engine } = useDatabaseStore(
    useShallow((s) => ({
      target: s.ddlTarget,
      close: s.closeDDL,
      profileId: s.selectedProfileId,
      engine: s.profiles.find((p) => p.id === s.selectedProfileId)?.engine,
    })),
  );

  const [ddl, setDDL] = useState("");
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!target || !profileId) {
      return;
    }
    let cancelled = false;
    setDDL("");
    setError("");
    setCopied(false);
    tableDDL(profileId, target.schema, target.table)
      .then((statement) => {
        if (!cancelled) setDDL(statement);
      })
      .catch((failure: unknown) => {
        if (!cancelled) setError(errorMessage(failure));
      });
    return () => {
      cancelled = true;
    };
  }, [target, profileId]);

  const extensions = useMemo(() => {
    const normalised = (engine ?? "postgres").toLowerCase();
    const dialect =
      normalised === "mysql" ? MySQL : normalised === "mariadb" ? MariaSQL : PostgreSQL;
    return [codemirrorSql({ dialect })];
  }, [engine]);

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {target ? `${target.schema}.${target.table}` : "Table definition"}
          </DialogTitle>
          <DialogDescription>
            What the server would use to recreate this table, in its own words.
          </DialogDescription>
        </DialogHeader>

        {error ? (
          <EmptyState
            icon={Copy}
            tone="danger"
            title="No definition available"
            body={error}
          />
        ) : ddl ? (
          <div className="h-96 overflow-auto rounded-md border border-border-subtle bg-surface-well p-2">
            <CodeEditor value={ddl} onChange={() => {}} extensions={extensions} readOnly wrap />
          </div>
        ) : (
          <Skeleton className="h-96 w-full" />
        )}

        <DialogFooter>
          <Button
            size="sm"
            disabled={!ddl}
            onClick={() => {
              void copyText(ddl).then(() => setCopied(true));
            }}
          >
            {copied ? <Check /> : <Copy />}
            {copied ? "Copied" : "Copy"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
