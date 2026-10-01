import { useEffect, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { FileUp } from "lucide-react";
import {
  importPostmanCollection,
  importPostmanEnvironment,
  parseCurl,
  type ImportResult,
} from "@/bridge/apiclient";
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

export type ImportKind = "curl" | "collection" | "environment";

/** Reads a chosen file as text into the paste box. */
function FilePicker({ onText }: { onText: (text: string, name: string) => void }) {
  const input = useRef<HTMLInputElement | null>(null);
  return (
    <>
      <input
        ref={input}
        type="file"
        accept=".json,application/json"
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

/**
 * Bringing work in from elsewhere: a curl command (opens as a new, unsaved
 * request), a Postman collection export, a Postman environment or globals
 * export. Postman has no env tags, so an environment import asks for one —
 * the tag decides whether sending asks first, and guessing it is how a
 * production environment ends up ungated.
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
  const [kind, setKind] = useState<ImportKind>(initialKind);
  const [text, setText] = useState("");
  const [fileName, setFileName] = useState("");
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
    setError("");
    listEnvTags()
      .then((list) => {
        setTags(list);
        // The safest tag that exists is the default: one that asks first.
        setTag((current) => current || list.find((t) => !t.requiresConfirmation)?.name || list[0]?.name || "");
      })
      .catch(() => setTags([]));
  }, [open, initialKind]);

  async function run() {
    switch (kind) {
      case "curl": {
        const request = await parseCurl(text);
        openNewRequest(request);
        onImported(null);
        return;
      }
      case "collection": {
        const result = await importPostmanCollection(text);
        toast.success(`Imported “${result.name}”: ${summary(result)}`, {
          description: result.warnings?.length ? result.warnings.join(" ") : undefined,
        });
        onImported(result);
        return;
      }
      case "environment": {
        const result = await importPostmanEnvironment(text, tag);
        toast.success(`Imported “${result.name}”: ${summary(result)}`);
        onImported(result);
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onClose())}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Import</DialogTitle>
          <DialogDescription>
            Nothing is sent while importing. Scripts in a Postman collection are not run or kept.
          </DialogDescription>
        </DialogHeader>
        <Tabs value={kind} onValueChange={(value) => setKind(value as ImportKind)} className="flex flex-col gap-3">
          <TabsList className="w-fit">
            <TabsTrigger value="curl">cURL</TabsTrigger>
            <TabsTrigger value="collection">Postman collection</TabsTrigger>
            <TabsTrigger value="environment">Postman environment</TabsTrigger>
          </TabsList>
          <TabsContent value="curl" className="text-caption-11 text-text-tertiary">
            Paste a curl command — a browser’s “Copy as cURL” works. It opens as a new request you can edit before saving.
          </TabsContent>
          <TabsContent value="collection" className="flex items-center gap-2 text-caption-11 text-text-tertiary">
            <FilePicker onText={(value, name) => { setText(value); setFileName(name); }} />
            <span className="truncate">{fileName || "A v2.0 or v2.1 collection export (.json), or paste it below."}</span>
          </TabsContent>
          <TabsContent value="environment" className="flex flex-col gap-2 text-caption-11 text-text-tertiary">
            <div className="flex items-center gap-2">
              <FilePicker onText={(value, name) => { setText(value); setFileName(name); }} />
              <span className="truncate">{fileName || "An environment or globals export (.json), or paste it below."}</span>
            </div>
            <label className="flex items-center gap-2">
              <span className="shrink-0">Environment tag</span>
              <NativeSelect size="sm" value={tag} onChange={(event) => setTag(event.target.value)} aria-label="Environment tag">
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
            disabled={!text.trim() || busy || (kind === "environment" && !tag)}
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
