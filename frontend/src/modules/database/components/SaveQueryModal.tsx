import { useId } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldLabel, FieldError } from "@/components/ui/field";
import { InlineFeedback, PendingStatus } from "@/components/ui/feedback";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { CodeBlock } from "@/components/code/CodeBlock";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { useDatabaseStore } from "../databaseStore";
import { runDraftOperation, useDatabaseDrafts } from "../documentDrafts";
import { saveQuery } from "@/bridge/database";
import { errorMessage } from "@/bridge/errors";

export function SaveQueryModal() {
  const client = useQueryClient();
  const visible = useModuleVisibility();
  const draft = useDatabaseDrafts((s) => s.save);
  const show = useDatabaseStore((s) => s.showSaveQueryModal);
  const id = useId();
  if (!draft) return null;
  const patch = (value: Partial<typeof draft>) =>
    useDatabaseDrafts.getState().patchSave(draft.id, value);
  const close = () => useDatabaseStore.getState().setShowSaveQueryModal(false);
  return (
    <Dialog
      open={show && visible}
      onOpenChange={(open) => {
        if (!open && visible && !draft.pending) close();
      }}
    >
      <DialogContent
        showCloseButton={!draft.pending}
        className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl"
      >
        <DialogHeader>
          <DialogTitle>Save query</DialogTitle>
          <DialogDescription>
            Name this statement snapshot. Saving does not execute SQL; it stores
            an encrypted query against{" "}
            {draft.profileName || "the original connection"}.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!draft.name.trim()) {
              patch({ error: "Enter a query name." });
              e.currentTarget.querySelector<HTMLInputElement>("input")?.focus();
              return;
            }
            if (!draft.profileId || !draft.sql.trim()) {
              patch({
                error:
                  "Open a console with a connection and SQL before saving.",
              });
              return;
            }
            void runDraftOperation("save", async (snapshot) => {
              try {
                await saveQuery(
                  snapshot.profileId,
                  snapshot.name.trim(),
                  snapshot.sql,
                );
                void client.invalidateQueries({
                  queryKey: ["database", "saved", snapshot.profileId],
                });
                useDatabaseDrafts
                  .getState()
                  .patchSave(snapshot.id, { pending: false });
                if (useDatabaseDrafts.getState().save?.id === snapshot.id)
                  close();
              } catch (err) {
                useDatabaseDrafts
                  .getState()
                  .patchSave(snapshot.id, { error: errorMessage(err) });
              }
            });
          }}
        >
          <Field>
            <FieldLabel htmlFor={id}>Query name</FieldLabel>
            <Input
              id={id}
              autoFocus
              value={draft.name}
              disabled={draft.pending}
              onChange={(e) => patch({ name: e.target.value, error: "" })}
              aria-invalid={!!draft.error && !draft.name.trim()}
              aria-describedby={draft.error ? `${id}-error` : undefined}
            />
            {draft.error && !draft.name.trim() ? (
              <FieldError id={`${id}-error`}>{draft.error}</FieldError>
            ) : null}
          </Field>
          <div className="max-h-64 overflow-auto rounded-md border border-border bg-surface-raised p-3">
            <CodeBlock
              code={draft.sql.slice(0, 12000)}
              language="sql"
              className="whitespace-pre-wrap break-words text-code-12"
            />
          </div>
          {draft.sql.length > 12000 ? (
            <p className="text-body-12 text-text-secondary">
              Preview shortened. The complete statement will be saved.
            </p>
          ) : null}
          {draft.error && draft.name.trim() ? (
            <InlineFeedback id={`${id}-error`}>{draft.error}</InlineFeedback>
          ) : null}
          {draft.pending ? <PendingStatus>Saving query…</PendingStatus> : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={draft.pending}
              onClick={close}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={draft.pending}>
              {draft.pending ? "Saving…" : "Save query"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
