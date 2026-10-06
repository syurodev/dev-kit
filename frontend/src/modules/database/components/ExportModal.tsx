import { useId } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import {
  Field,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { useDatabaseStore } from "../databaseStore";
import { runDraftOperation, useDatabaseDrafts } from "../documentDrafts";
import { exportQueryResult, exportFilenameError } from "../export";
import { errorMessage } from "@/bridge/errors";

export function ExportModal() {
  const draft = useDatabaseDrafts((s) => s.export);
  const show = useDatabaseStore((s) => s.showExportModal);
  const visible = useModuleVisibility();
  const id = useId();
  if (!draft) return null;
  const patch = (value: Partial<typeof draft>) =>
    useDatabaseDrafts.getState().patchExport(draft.id, value);
  const close = () => useDatabaseStore.getState().setShowExportModal(false);
  const invalid = exportFilenameError(draft.filename);
  return (
    <Dialog
      open={show && visible}
      onOpenChange={(open) => {
        if (!open && visible && !draft.pending) close();
      }}
    >
      <DialogContent
        showCloseButton={!draft.pending}
        className="max-h-[85dvh] overflow-y-auto sm:max-w-lg"
      >
        <DialogHeader>
          <DialogTitle>Export loaded rows</DialogTitle>
          <DialogDescription>
            {draft.result.rows.length.toLocaleString()} rows ·{" "}
            {draft.result.columns.length} columns. This snapshot includes only
            loaded rows, not the whole table. The native file picker chooses the
            destination.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (invalid) {
              patch({ error: invalid });
              e.currentTarget.querySelector<HTMLInputElement>("input")?.focus();
              return;
            }
            void runDraftOperation("export", async (snapshot) => {
              try {
                const filename =
                  snapshot.filename.trim().replace(/\.(csv|json)$/i, "") +
                  `.${snapshot.format}`;
                const path = await exportQueryResult(
                  snapshot.result,
                  snapshot.format,
                  filename,
                );
                useDatabaseDrafts.getState().patchExport(snapshot.id, {
                  status: path
                    ? `Saved to ${path}`
                    : "File picker canceled. No file was written.",
                });
              } catch (err) {
                useDatabaseDrafts
                  .getState()
                  .patchExport(snapshot.id, { error: errorMessage(err) });
              }
            });
          }}
        >
          <Field>
            <FieldLabel htmlFor={`${id}-format`}>Format</FieldLabel>
            <NativeSelect
              id={`${id}-format`}
              disabled={draft.pending}
              value={draft.format}
              onChange={(e) =>
                patch({
                  format: e.target.value as "csv" | "json",
                  error: "",
                  status: "",
                })
              }
            >
              <option value="csv">CSV</option>
              <option value="json">JSON</option>
            </NativeSelect>
            <FieldDescription>
              {draft.format === "csv"
                ? "CSV quotes delimiters and line breaks; SQL NULL and empty text both export as empty fields."
                : "JSON keeps values as strings and SQL NULL as null; numbers are not guessed or rounded."}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor={id}>Suggested filename</FieldLabel>
            <Input
              id={id}
              value={draft.filename}
              autoFocus
              disabled={draft.pending}
              onChange={(e) =>
                patch({ filename: e.target.value, error: "", status: "" })
              }
              aria-invalid={!!draft.error && !!invalid}
              aria-describedby={draft.error ? `${id}-error` : undefined}
            />
            <FieldDescription>
              The .{draft.format} extension is added automatically.
            </FieldDescription>
            {draft.error && invalid ? (
              <FieldError id={`${id}-error`}>{draft.error}</FieldError>
            ) : null}
          </Field>
          {draft.error && !invalid ? (
            <InlineFeedback id={`${id}-error`}>{draft.error}</InlineFeedback>
          ) : null}
          {draft.status ? (
            <InlineFeedback tone="info">{draft.status}</InlineFeedback>
          ) : null}
          {draft.pending ? (
            <PendingStatus>
              Preparing export / waiting for the file picker…
            </PendingStatus>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={close}
              disabled={draft.pending}
            >
              {draft.status ? "Close" : "Cancel"}
            </Button>
            <Button type="submit" disabled={draft.pending}>
              Save file…
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
