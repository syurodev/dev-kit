import { useEffect, useMemo, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Copy, Download, ShieldCheck } from "lucide-react";
import {
  exportFile,
  exportToFile,
  importFile,
  listFormats,
  parseCurl,
  type ExportScope,
  type FormatInfo,
  type ImportResult,
} from "@/bridge/apiclient";
import { copyText } from "@/bridge/clipboard";
import { listEnvTags, type EnvTag } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { ImportDialogView } from "./ImportDialogView";
import { openNewRequest } from "./documents";

/**
 * A one-field form: a folder's name, a request's new name. A refused submit
 * keeps the dialog open with the reason — closing first would lose what was
 * typed and turn the error into a toast about a form that is gone.
 */
export function NameDialog({
  open,
  visible = true,
  title,
  initial = "",
  placeholder,
  submitLabel = "Save",
  onSubmit,
  onClose,
}: {
  open: boolean;
  visible?: boolean;
  title: ReactNode;
  initial?: string;
  placeholder?: string;
  submitLabel?: string;
  onSubmit: (name: string) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(initial);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (open) {
      setName(initial);
      setError("");
    }
  }, [open, initial]);

  return (
    <Dialog open={open && visible} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogContent className="sm:max-w-md" showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            if (!name.trim() || busy) return;
            setBusy(true);
            onSubmit(name.trim())
              .then(onClose)
              .catch((e) => setError(errorMessage(e)))
              .finally(() => setBusy(false));
          }}
        >
          <label className="flex flex-col gap-2 text-caption-11-medium text-text-secondary">Name<Input autoFocus value={name} disabled={busy} placeholder={placeholder} onChange={(event) => setName(event.target.value)} /></label>
          {error ? (
            <p role="alert" className="text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
          <DialogFooter className="mt-4">
            <Button type="button" variant="ghost" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || busy}>
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** "Are you sure", for the deletes a vault item cannot undo. */
export function ConfirmDialog({
  open,
  visible = true,
  title,
  description,
  confirmLabel = "Delete",
  onConfirm,
  onClose,
}: {
  open: boolean;
  visible?: boolean;
  title: ReactNode;
  description: ReactNode;
  confirmLabel?: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <AlertDialog open={open && visible} onOpenChange={(next) => (next ? undefined : onClose())}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={onConfirm}>
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export type ImportKind = "curl" | "file";

function summary(result: ImportResult): string {
  const parts: string[] = [];
  if (result.requests) parts.push(`${result.requests} request${result.requests === 1 ? "" : "s"}`);
  if (result.folders) parts.push(`${result.folders} folder${result.folders === 1 ? "" : "s"}`);
  if (result.variables) parts.push(`${result.variables} variable${result.variables === 1 ? "" : "s"}`);
  return parts.length ? parts.join(", ") : "nothing";
}

/** The formats Go registered, loaded once per open dialog. null until known. */
function useFormats(open: boolean): FormatInfo[] | null {
  const [formats, setFormats] = useState<FormatInfo[] | null>(null);
  useEffect(() => {
    if (!open) return;
    listFormats()
      .then(setFormats)
      .catch(() => setFormats([]));
  }, [open]);
  return formats;
}

/**
 * Bringing work in from elsewhere: a curl command (opens as a new, unsaved
 * request — a draft, not an import) or a file in any format Go can read,
 * detected or chosen.
 *
 * The tag question is asked up front for any file, because a file may hold an
 * environment and the tag decides whether sending asks first. "From the file"
 * uses one the file names (DevKit writes it into an exported environment's
 * name) when this machine has it, and the import is refused otherwise rather
 * than guessing.
 */
export function ImportDialog({
  open,
  visible = true,
  initialKind = "curl",
  onClose,
  onImported,
}: {
  open: boolean;
  visible?: boolean;
  initialKind?: ImportKind;
  onClose: () => void;
  onImported: (result: ImportResult | null) => void;
}) {
  const formats = useFormats(open);
  const [kind, setKind] = useState<ImportKind>(initialKind);
  const [texts, setTexts] = useState<Record<ImportKind, string>>({ curl: "", file: "" });
  const text = texts[kind];
  const setText = (value: string) => setTexts((current) => ({ ...current, [kind]: value }));
  const [fileName, setFileName] = useState("");
  const [format, setFormat] = useState("");
  // null until the list answers, so the picker never offers "no tags" first.
  const [tags, setTags] = useState<EnvTag[] | null>(null);
  const [tag, setTag] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setKind(initialKind);
    setTexts({ curl: "", file: "" });
    setFileName("");
    setFormat("");
    setTag("");
    setError("");
    listEnvTags()
      .then(setTags)
      .catch(() => setTags([]));
  }, [open, initialKind]);

  const importable = (formats ?? []).filter((f) => f.canImport);

  async function run() {
    if (kind === "curl") {
      const request = await parseCurl(text);
      openNewRequest(request);
      onImported(null);
      return;
    }
    const result = await importFile(format, text, tag);
    const label = formats?.find((f) => f.id === result.format)?.label ?? result.format;
    toast.success(`Imported “${result.name}” from ${label}: ${summary(result)}`, {
      description: result.warnings?.length ? result.warnings.join(" ") : undefined,
    });
    onImported(result);
  }

  return <ImportDialogView
    open={open && visible} kind={kind} onKindChange={setKind} text={text} onTextChange={setText}
    fileName={fileName} onFile={(value, name) => { setText(value); setFileName(name); }}
    format={format} onFormatChange={setFormat} formats={importable}
    tag={tag} onTagChange={setTag} tags={tags ?? []}
    error={error} onError={setError} busy={busy} onClose={onClose}
    onSubmit={() => {
      if (busy || !text.trim()) return;
      setBusy(true);
      setError("");
      run().then(onClose).catch((e) => setError(errorMessage(e))).finally(() => setBusy(false));
    }}
  />;
}

/**
 * Writing a collection, an environment or the globals out in any format that
 * can hold it. What leaves is decided in Go for every format alike: no secret
 * value (its name, empty), no credential (a variable in its place). The
 * dialog says so before and repeats what the export reported after.
 */
export function ExportDialog({
  scope,
  visible = true,
  title,
  onClose,
}: {
  /** null is closed. */
  scope: ExportScope | null;
  visible?: boolean;
  title: string;
  onClose: () => void;
}) {
  const open = scope !== null;
  const formats = useFormats(open);
  const [format, setFormat] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [warnings, setWarnings] = useState<string[] | null>(null);

  const usable = useMemo(
    () => (formats ?? []).filter((f) => scope && (f.exports ?? []).includes(scope.kind)),
    [formats, scope],
  );
  useEffect(() => {
    if (!open) return;
    setError("");
    setWarnings(null);
  }, [open]);
  useEffect(() => {
    if (usable.length && !usable.some((f) => f.id === format)) setFormat(usable[0].id);
  }, [usable, format]);

  function finish(fileWarnings: string[] | null, said: string) {
    toast.success(said);
    if (fileWarnings?.length) setWarnings(fileWarnings);
    else onClose();
  }

  async function save() {
    if (!scope) return;
    const saved = await exportToFile(scope, format);
    if (!saved.path) return;
    finish(saved.warnings, `Saved ${saved.path}`);
  }

  async function copy() {
    if (!scope) return;
    const file = await exportFile(scope, format);
    await copyText(file.content);
    finish(file.warnings, `${file.fileName} copied`);
  }

  function attempt(work: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    work()
      .catch((e) => setError(errorMessage(e)))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={open && visible} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogContent className="gap-5 sm:max-w-md" showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>Export collection or variables</DialogTitle>
          <DialogDescription>
            Choose a format to save a file or copy its contents.
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-3 rounded-lg border border-border-subtle bg-surface-well p-3"><Download className="size-5 shrink-0 text-text-tertiary" /><div className="min-w-0"><p className="truncate text-body-13-medium">{title}</p><p className="mt-0.5 text-caption-11 text-text-tertiary">{scope?.kind === "globals" ? "Global variables" : scope?.kind === "environment" ? "Environment variables" : "Collection and requests"}</p></div></div>
        {warnings ? (
          <ul className="flex list-disc flex-col gap-1 pl-4 text-caption-11 text-text-secondary">
            {warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        ) : (
          <label className="flex flex-col gap-2 text-caption-11-medium text-text-secondary">
            <span className="shrink-0">Format</span>
            <NativeSelect className="w-full" disabled={busy} value={format} onChange={(event) => setFormat(event.target.value)} aria-label="Format">
              {usable.map((f) => (
                <NativeSelectOption key={f.id} value={f.id}>
                  {f.label} (.{f.extension})
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </label>
        )}
        <p className="flex items-start gap-2 text-caption-11 text-text-tertiary"><ShieldCheck className="size-3.5 shrink-0" /><span>Secret values are excluded. Account credentials are replaced with variable references.</span></p>
        {error ? (
          <p role="alert" className="text-caption-11 text-status-danger">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          {warnings ? (
            <Button type="button" onClick={onClose}>
              Done
            </Button>
          ) : (
            <>
              <Button type="button" variant="ghost" disabled={busy} onClick={onClose}>
                Cancel
              </Button>
              <Button type="button" variant="outline" disabled={!format || busy} onClick={() => attempt(copy)}>
                <Copy /> Copy
              </Button>
              <Button type="button" disabled={!format || busy} onClick={() => attempt(save)}>
                <Download /> {busy ? "Exporting…" : "Save file…"}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
