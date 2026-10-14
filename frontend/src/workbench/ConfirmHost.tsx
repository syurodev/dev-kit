import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { registerConfirmHost, type ConfirmRequest } from "./confirmRequest";

type Pending = ConfirmRequest & { answer: (yes: boolean) => void };

/**
 * Shows the questions asked through `requestConfirm`, one at a time. A
 * question asked while another is open is answered no rather than queued
 * behind it: it was asked about a moment that has passed.
 */
export function ConfirmHost() {
  const [pending, setPending] = useState<Pending | null>(null);

  useEffect(
    () =>
      registerConfirmHost(
        (request) =>
          new Promise<boolean>((resolve) => {
            setPending((current) => {
              if (current) {
                resolve(false);
                return current;
              }
              return { ...request, answer: resolve };
            });
          }),
      ),
    [],
  );

  const settle = (yes: boolean) => {
    pending?.answer(yes);
    setPending(null);
  };

  return (
    <ConfirmDialog
      open={pending !== null}
      onClose={() => settle(false)}
      title={pending?.title ?? ""}
      confirmLabel={pending?.confirmLabel ?? ""}
      pendingLabel=""
      onConfirm={() => settle(true)}
    >
      {pending?.description}
    </ConfirmDialog>
  );
}
