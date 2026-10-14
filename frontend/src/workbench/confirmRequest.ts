/**
 * A yes/no question asked from code that has no component of its own — a
 * module's intent handler, an action started in another module's view.
 * Same shape as settingsRequest/vaultUnlockRequest: ConfirmHost, mounted
 * once by the Workbench, owns the dialog (components/ui/confirm-dialog) and
 * answers. With no host mounted the answer is no: a question nobody can see
 * must not be taken as consent.
 */
export type ConfirmRequest = {
  title: string;
  description: string;
  /** The destructive action's label; Cancel is always offered and focused. */
  confirmLabel: string;
};

type Host = (request: ConfirmRequest) => Promise<boolean>;

let host: Host | null = null;

export function requestConfirm(request: ConfirmRequest): Promise<boolean> {
  return host ? host(request) : Promise.resolve(false);
}

/** ConfirmHost registers itself; the returned function unregisters it. */
export function registerConfirmHost(next: Host): () => void {
  host = next;
  return () => {
    if (host === next) host = null;
  };
}
