import type { ReactNode } from "react";
import { Button } from "./button";
import { InlineFeedback, PendingStatus } from "./feedback";
import { AlertDialog, AlertDialogContent, AlertDialogHeader, AlertDialogTitle, AlertDialogDescription, AlertDialogFooter, AlertDialogCancel } from "./alert-dialog";

/** Confirmation stays open on failure; the safe Cancel action receives focus. */
export function ConfirmDialog({ open, onClose, title, children, confirmLabel, pendingLabel, pending = false, error, onConfirm }: {
  open: boolean; onClose: () => void; title: string; children: ReactNode;
  confirmLabel: string; pendingLabel: string; pending?: boolean; error?: string; onConfirm: () => void;
}) {
  return <AlertDialog open={open} onOpenChange={value => { if (!value && !pending) onClose(); }}>
    <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{title}</AlertDialogTitle><AlertDialogDescription>{children}</AlertDialogDescription></AlertDialogHeader>
      {error && <InlineFeedback>{error}</InlineFeedback>}
      {pending && <PendingStatus>{pendingLabel}</PendingStatus>}
      <AlertDialogFooter><AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
        <Button variant="destructive" disabled={pending} onClick={onConfirm}>{pending ? pendingLabel : confirmLabel}</Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
}
