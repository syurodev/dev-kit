import { useMemo, useRef, useState } from "react";
import {
  sql as codemirrorSql,
  MariaSQL,
  MySQL,
  PostgreSQL,
} from "@codemirror/lang-sql";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { errorMessage, isCancelError } from "@/bridge/errors";

/** Review a snapshot; a failed apply stays in the same decision region. */
export function ApplyEditsDialog({
  open,
  visible = true,
  env,
  engine,
  manual,
  statements,
  onCancel,
  onApply,
}: {
  open: boolean;
  visible?: boolean;
  env: string;
  engine: string | undefined;
  manual: boolean;
  statements: readonly string[];
  onCancel: () => void;
  onApply: () => Promise<void>;
}) {
  const [applying, setApplying] = useState(false);
  const operation = useRef(false);
  const [error, setError] = useState("");
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setError("");
  }
  const extensions = useMemo(() => {
    const normalised = (engine ?? "postgres").toLowerCase();
    return [
      codemirrorSql({
        dialect:
          normalised === "mysql"
            ? MySQL
            : normalised === "mariadb"
              ? MariaSQL
              : PostgreSQL,
      }),
    ];
  }, [engine]);
  const count = `${statements.length} row update${statements.length === 1 ? "" : "s"}`;
  return (
    <AlertDialog
      open={open && visible}
      onOpenChange={(next) => {
        if (!next && !operation.current) onCancel();
      }}
    >
      <AlertDialogContent className="max-h-[90dvh] overflow-y-auto data-[size=default]:max-w-gutter data-[size=default]:sm:max-w-3xl">
        <AlertDialogHeader>
          <AlertDialogTitle>
            Apply {count} to {env}?
          </AlertDialogTitle>
          <AlertDialogDescription>
            {manual
              ? "Changes join the open transaction. Commit to keep them."
              : "Changes run in one transaction. If a statement fails or matches no row, none is kept."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="h-64 min-h-0 overflow-hidden rounded-md border border-border-subtle bg-surface-well p-2">
          <CodeEditor
            value={statements.map((s) => `${s};`).join("\n")}
            onChange={() => {}}
            extensions={extensions}
            readOnly
            wrap
          />
        </div>
        <div className="min-h-5">
          {error ? (
            <InlineFeedback>{error}</InlineFeedback>
          ) : applying ? (
            <PendingStatus>Applying changes…</PendingStatus>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={applying}>Cancel</AlertDialogCancel>
          <Button
            size="sm"
            disabled={applying || statements.length === 0}
            onClick={() => {
              if (operation.current) return;
              operation.current = true;
              setApplying(true);
              setError("");
              void onApply()
                .catch((e) => {
                  if (!isCancelError(e)) setError(errorMessage(e));
                })
                .finally(() => {
                  operation.current = false;
                  setApplying(false);
                });
            }}
          >
            {applying ? "Applying…" : `Apply ${count}`}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
