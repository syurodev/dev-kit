import { useState } from "react";
import { Bookmark, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useShallow } from "zustand/react/shallow";
import { useDatabaseStore } from "../databaseStore";
import { saveQuery } from "../../../bridge/database";
import { errorMessage } from "../../../bridge/errors";

export function SaveQueryModal() {
  const {
    selectedProfileId,
    sql,
    showSaveQueryModal,
    setShowSaveQueryModal,
  } = useDatabaseStore(
    useShallow((s) => ({
      selectedProfileId: s.selectedProfileId,
      sql: s.sql,
      showSaveQueryModal: s.showSaveQueryModal,
      setShowSaveQueryModal: s.setShowSaveQueryModal,
    }))
  );

  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  if (!showSaveQueryModal) return null;

  async function handleSave() {
    if (!selectedProfileId || !name.trim() || !sql.trim()) return;
    setSaving(true);
    setError("");
    try {
      await saveQuery(selectedProfileId, name.trim(), sql);
      setShowSaveQueryModal(false);
      setName("");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-xl w-full max-w-md p-5 flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-(--motion-base)">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Bookmark className="w-4 h-4 text-action-primary" />
            <h3 className="text-title-14 text-text-primary">Save Query</h3>
          </div>
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 text-text-tertiary hover:text-text-primary"
            onClick={() => setShowSaveQueryModal(false)}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        <p className="text-body-12 text-text-secondary">
          Give this statement a memorable name. Saved queries sync securely with your vault across devices.
        </p>

        <div className="space-y-2">
          <label className="text-label-12 text-text-primary">Query Name</label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Monthly active users count"
            className="text-label-12 h-8"
            autoFocus
          />
        </div>

        <div className="space-y-1">
          <label className="text-caption-11-medium text-text-tertiary">SQL Preview</label>
          <pre className="text-code-11 text-text-secondary bg-surface-raised p-2 rounded border border-border max-h-32 overflow-y-auto whitespace-pre-wrap">
            {sql}
          </pre>
        </div>

        {error && (
          <div className="p-2 rounded bg-status-danger/10 border border-status-danger/30 text-body-12 text-status-danger">
            {error}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-2 border-t border-border">
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
        </div>
      </div>
    </div>
  );
}
