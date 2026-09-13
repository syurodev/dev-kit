import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Bookmark } from "lucide-react";
import { sql as codemirrorSql, PostgreSQL, MySQL, MariaSQL } from "@codemirror/lang-sql";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useShallow } from "zustand/react/shallow";
import { EMPTY_CONSOLE, useDatabaseStore } from "../databaseStore";
import { saveQuery } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

export function SaveQueryModal() {
  const queryClient = useQueryClient();
  // Not named `console`: shadowing the global inside a component is a trap.
  const modalConsoleId = useDatabaseStore((s) => s.modalConsoleId);
  const slice = useDatabaseStore((s) => s.consoles[modalConsoleId] ?? EMPTY_CONSOLE);
  const {
    selectedProfileId,
    showSaveQueryModal,
    setShowSaveQueryModal,
  } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      showSaveQueryModal: s.showSaveQueryModal,
      setShowSaveQueryModal: s.setShowSaveQueryModal,
    }))
  );
  const sql = slice.sql;
  const engine = useDatabaseStore(
    (s) => s.profiles.find((p) => p.id === s.selectedProfileId)?.engine,
  );

  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  // The console's own editor and dialect, read-only: the preview should look
  // like the statement being named, not like a log of it.
  const sqlExtensions = useMemo(() => {
    const kind = (engine || "postgres").toLowerCase();
    const dialect = kind === "mysql" ? MySQL : kind === "mariadb" ? MariaSQL : PostgreSQL;
    return [codemirrorSql({ dialect, upperCaseKeywords: true })];
  }, [engine]);

  async function handleSave() {
    if (!selectedProfileId || !name.trim() || !sql.trim()) return;
    setSaving(true);
    setError("");
    try {
      await saveQuery(selectedProfileId, name.trim(), sql);
      void queryClient.invalidateQueries({ queryKey: ["database", "saved", selectedProfileId] });
      setShowSaveQueryModal(false);
      setName("");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={showSaveQueryModal} onOpenChange={setShowSaveQueryModal}>
      <DialogContent className="gap-4 p-5 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-title-14 text-text-primary">
            <Bookmark className="size-4 text-action-primary" />
            Save Query
          </DialogTitle>
          <DialogDescription className="text-body-12 text-text-secondary">
            Give this statement a memorable name. Saved queries sync securely with your vault across devices.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-2">
          <label htmlFor="save-query-name" className="text-label-12 text-text-primary">
            Query Name
          </label>
          <Input
            id="save-query-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Monthly active users count"
            className="text-label-12 h-8"
            autoFocus
          />
        </div>

        <div className="space-y-1">
          <span className="text-caption-11-medium text-text-tertiary">SQL Preview</span>
          <div className="h-72 overflow-hidden rounded-md border border-border bg-surface-raised">
            <CodeEditor value={sql} onChange={() => {}} extensions={sqlExtensions} readOnly wrap />
          </div>
        </div>

        {error && (
          <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger">
            {error}
          </div>
        )}

        <DialogFooter className="-mx-5 -mb-5 px-5">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowSaveQueryModal(false)}
            disabled={saving}
          >
            Cancel
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={handleSave}
            disabled={saving || !name.trim()}
          >
            {saving ? "Saving…" : "Save Query"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
