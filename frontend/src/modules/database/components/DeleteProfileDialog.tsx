import { useRef, useState } from "react";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { errorMessage } from "@/bridge/errors";
import type { ConnectionProfile } from "@/bridge/database";
import { useLastPresent } from "@/lib/useLastPresent";

export function DeleteProfileDialog({
  profile,
  onClose,
  onConfirm,
}: {
  profile: ConnectionProfile | null;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const shown = useLastPresent(profile);
  const active = useWorkbenchTabsStore((s) => s.activeModuleId === "database");
  const [pending, setPending] = useState(false);
  const operation = useRef(false);
  const [error, setError] = useState("");
  const [targetId, setTargetId] = useState(profile?.id);
  if (targetId !== profile?.id) {
    setTargetId(profile?.id);
    setError("");
  }
  return (
    <ConfirmDialog
      open={Boolean(profile) && active}
      onClose={() => {
        if (!operation.current) {
          setError("");
          onClose();
        }
      }}
      title={`Delete ${shown?.name ?? "connection"}?`}
      confirmLabel="Delete profile"
      pendingLabel="Deleting profile…"
      pending={pending}
      error={error}
      onConfirm={() => {
        if (operation.current) return;
        operation.current = true;
        setPending(true);
        setError("");
        void onConfirm()
          .catch((e) => setError(errorMessage(e)))
          .finally(() => {
            operation.current = false;
            setPending(false);
          });
      }}
    >
      This removes the saved password, local query history, saved queries and
      folder membership from DevKit. The database and its tables stay unchanged.
    </ConfirmDialog>
  );
}
