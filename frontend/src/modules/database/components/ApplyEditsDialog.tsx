import { useMemo, useState } from "react";
import { sql as codemirrorSql, MariaSQL, MySQL, PostgreSQL } from "@codemirror/lang-sql";
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

/**
 * The statements a review is about to write, shown before anything runs (#52).
 *
 * On an environment that asks for review a grid edit is staged, not written;
 * this is where the staged changes become SQL someone can read. Apply sends all
 * of them in one transaction — see Service.ApplyEdits.
 */
export function ApplyEditsDialog({
  open,
  env,
  engine,
  manual,
  statements,
  onCancel,
  onApply,
}: {
  open: boolean;
  env: string;
  engine: string | undefined;
  /** Manual commit: the changes join the open transaction and wait for Commit. */
  manual: boolean;
  statements: readonly string[];
  onCancel: () => void;
  onApply: () => Promise<void>;
}) {
  const [applying, setApplying] = useState(false);
  const extensions = useMemo(() => {
    const normalised = (engine ?? "postgres").toLowerCase();
    const dialect =
      normalised === "mysql" ? MySQL : normalised === "mariadb" ? MariaSQL : PostgreSQL;
    return [codemirrorSql({ dialect })];
  }, [engine]);
  const count = `${statements.length} change${statements.length === 1 ? "" : "s"}`;

  return (
    <Dialog open={open} onOpenChange={(next) => !next && !applying && onCancel()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            Apply {count} to {env}?
          </DialogTitle>
          <DialogDescription>
            {manual
              ? "They run inside the open transaction and are not kept until you commit it."
              : "They run in one transaction. If any of them fails or matches no row, none of them is kept."}
          </DialogDescription>
        </DialogHeader>

        <div className="h-72 overflow-auto rounded-md border border-border-subtle bg-surface-well p-2">
          <CodeEditor
            value={statements.map((statement) => `${statement};`).join("\n")}
            onChange={() => {}}
            extensions={extensions}
            readOnly
            wrap
          />
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onCancel} disabled={applying}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={applying || statements.length === 0}
            onClick={async () => {
              setApplying(true);
              try {
                await onApply();
              } finally {
                setApplying(false);
              }
            }}
          >
            Apply {count}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
