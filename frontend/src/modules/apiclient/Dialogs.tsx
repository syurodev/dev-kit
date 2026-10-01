import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { Copy, Download, FileUp } from "lucide-react";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { openNewRequest } from "./documents";

/**
 * A one-field form: a folder's name, a request's new name. A refused submit
 * keeps the dialog open with the reason — closing first would lose what was
 * typed and turn the error into a toast about a form that is gone.
 */
export function NameDialog({
  open,
  title,
  initial = "",
  placeholder,
  submitLabel = "Save",
  onSubmit,
  onClose,
}: {
  open: boolean;
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
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
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
          <Input autoFocus value={name} placeholder={placeholder} onChange={(event) => setName(event.target.value)} />
          {error ? (
            <p role="alert" className="text-caption-11 text-status-danger">
              {error}
            </p>
          ) : null}
          <DialogFooter className="mt-4">
            <Button type="button" variant="ghost" onClick={onClose}>
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
  title,
  description,
  confirmLabel = "Delete",
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: ReactNode;
  description: ReactNode;
  confirmLabel?: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
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

/** Reads a chosen file as text into the paste box. */
function FilePicker({ onText }: { onText: (text: string, name: string) => void }) {
  const input = useRef<HTMLInputElement | null>(null);
  return (
    <>
      <input
        ref={input}
        type="file"
        className="hidden"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (!file) return;
          void file.text().then((text) => onText(text, file.name));
          event.target.value = "";
        }}
      />
      <Button type="button" variant="outline" size="sm" onClick={() => input.current?.click()}>
        <FileUp /> Choose file…
      </Button>
    </>
  );
}

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
  initialKind = "curl",
  onClose,
  onImported,
}: {
  open: boolean;
  initialKind?: ImportKind;
  onClose: () => void;
  onImported: (result: ImportResult | null) => void;
}) {
  const formats = useFormats(open);
  const [kind, setKind] = useState<ImportKind>(initialKind);
  const [text, setText] = useState("");
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
    setText("");
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

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Import</DialogTitle>
          <DialogDescription>Nothing is sent while importing, and no script in a file is run or kept.</DialogDescription>
        </DialogHeader>
        <Tabs value={kind} onValueChange={(value) => setKind(value as ImportKind)} className="flex flex-col gap-3">
          <TabsList className="w-fit">
            <TabsTrigger value="curl">cURL</TabsTrigger>
            <TabsTrigger value="file">File</TabsTrigger>
          </TabsList>
          <TabsContent value="curl" className="text-caption-11 text-text-tertiary">
            Paste a curl command — a browser’s “Copy as cURL” works. It opens as a new request you can edit before saving.
          </TabsContent>
          <TabsContent value="file" className="flex flex-col gap-2 text-caption-11 text-text-tertiary">
            <div className="flex items-center gap-2">
              <FilePicker
                onText={(value, name) => {
                  setText(value);
                  setFileName(name);
                }}
              />
              <span className="truncate">{fileName || "Or paste the file below."}</span>
            </div>
            <label className="flex items-center gap-2">
              <span className="w-28 shrink-0">Format</span>
              <NativeSelect size="sm" value={format} onChange={(event) => setFormat(event.target.value)} aria-label="Format">
                <NativeSelectOption value="">Detect automatically</NativeSelectOption>
                {importable.map((f) => (
                  <NativeSelectOption key={f.id} value={f.id}>
                    {f.label}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
            <label className="flex items-center gap-2">
              <span className="w-28 shrink-0">Environments go to</span>
              <NativeSelect size="sm" value={tag} onChange={(event) => setTag(event.target.value)} aria-label="Environment tag">
                <NativeSelectOption value="">The tag in the file</NativeSelectOption>
                {(tags ?? []).map((t) => (
                  <NativeSelectOption key={t.id} value={t.name}>
                    {t.name}
                    {t.requiresConfirmation ? " — asks before every send" : ""}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </label>
          </TabsContent>
        </Tabs>
        <Textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          rows={8}
          spellCheck={false}
          placeholder={kind === "curl" ? "curl 'https://api.example.com/v1/users' -H 'Accept: application/json'" : "{ … }"}
          className="font-mono text-code-11"
          aria-label="Import text"
        />
        {error ? (
          <p role="alert" className="text-caption-11 text-status-danger">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!text.trim() || busy}
            onClick={() => {
              setBusy(true);
              setError("");
              run()
                .then(onClose)
                .catch((e) => setError(errorMessage(e)))
                .finally(() => setBusy(false));
            }}
          >
            {kind === "curl" ? "Open" : "Import"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Writing a collection, an environment or the globals out in any format that
 * can hold it. What leaves is decided in Go for every format alike: no secret
 * value (its name, empty), no credential (a variable in its place). The
 * dialog says so before and repeats what the export reported after.
 */
export function ExportDialog({
  scope,
  title,
  onClose,
}: {
  /** null is closed. */
  scope: ExportScope | null;
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
    setBusy(true);
    setError("");
    work()
      .catch((e) => setError(errorMessage(e)))
      .finally(() => setBusy(false));
  }

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Export {title}</DialogTitle>
          <DialogDescription>
            Secret values are never written — their names are, empty. A request that authenticates as an account names a
            variable in the credential’s place.
          </DialogDescription>
        </DialogHeader>
        {warnings ? (
          <ul className="flex list-disc flex-col gap-1 pl-4 text-caption-11 text-text-secondary">
            {warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        ) : (
          <label className="flex items-center gap-2 text-caption-11 text-text-tertiary">
            <span className="shrink-0">Format</span>
            <NativeSelect size="sm" value={format} onChange={(event) => setFormat(event.target.value)} aria-label="Format">
              {usable.map((f) => (
                <NativeSelectOption key={f.id} value={f.id}>
                  {f.label} (.{f.extension})
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </label>
        )}
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
              <Button type="button" variant="ghost" onClick={onClose}>
                Cancel
              </Button>
              <Button type="button" variant="outline" disabled={!format || busy} onClick={() => attempt(copy)}>
                <Copy /> Copy
              </Button>
              <Button type="button" disabled={!format || busy} onClick={() => attempt(save)}>
                <Download /> Save to file…
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
