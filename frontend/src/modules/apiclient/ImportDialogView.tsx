import { useEffect, useId, useRef, useState } from "react";
import {
  ArrowRight,
  FileCheck2,
  FileUp,
  Loader2,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import type { FormatInfo } from "@/bridge/apiclient";
import type { EnvTag } from "@/bridge/environments";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import type { ImportKind } from "./Dialogs";

/** Shared presentation for the real importer and its local design preview. */
export function ImportDialogView({
  open,
  kind,
  onKindChange,
  text,
  onTextChange,
  fileName,
  onFile,
  format,
  onFormatChange,
  formats,
  tag,
  onTagChange,
  tags,
  error,
  onError,
  busy,
  onClose,
  onSubmit,
}: {
  open: boolean;
  kind: ImportKind;
  onKindChange: (kind: ImportKind) => void;
  text: string;
  onTextChange: (text: string) => void;
  fileName: string;
  onFile: (text: string, name: string) => void;
  format: string;
  onFormatChange: (format: string) => void;
  formats: FormatInfo[];
  tag: string;
  onTagChange: (tag: string) => void;
  tags: EnvTag[];
  error: string;
  onError: (error: string) => void;
  busy: boolean;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const textId = useId();
  const [reading, setReading] = useState(false);
  const fileRead = useRef(0);
  useEffect(() => {
    setReading(false);
    return () => {
      fileRead.current += 1;
    };
  }, [open, kind]);
  const fileMode = kind === "file";
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) onClose();
      }}
    >
      <DialogContent
        className="flex max-h-[90dvh] flex-col gap-0 overflow-hidden p-0 sm:max-w-160"
        showCloseButton={!busy}
      >
        <DialogHeader className="gap-1.5 px-6 pt-6 pb-4 pr-12">
          <DialogTitle className="text-title-16">Import requests</DialogTitle>
          <DialogDescription className="text-body-13">
            Bring an existing request or collection into your workspace.
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 overflow-y-auto overscroll-contain px-6 pb-5">
          <Tabs
            className="gap-0"
            value={kind}
            onValueChange={(value) => {
              if (!busy && !reading) onKindChange(value as ImportKind);
            }}
          >
            <TabsList className="mb-4 w-full">
              <TabsTrigger
                className="flex-1 gap-2"
                value="curl"
                disabled={busy || reading}
              >
                <Terminal className="size-4" />
                Paste cURL
              </TabsTrigger>
              <TabsTrigger
                className="flex-1 gap-2"
                value="file"
                disabled={busy || reading}
              >
                <FileUp className="size-4" />
                Import file
              </TabsTrigger>
            </TabsList>
            <TabsContent value={kind} className="mt-0">
              <fieldset
                disabled={busy || reading}
                className="min-w-0 space-y-4"
              >
                {fileMode ? (
                  <>
                    <input
                      ref={input}
                      type="file"
                      className="hidden"
                      aria-label="Choose import file"
                      onChange={(event) => {
                        const file = event.target.files?.[0];
                        event.target.value = "";
                        if (!file) return;
                        const ticket = ++fileRead.current;
                        setReading(true);
                        void file
                          .text()
                          .then((value) => {
                            if (fileRead.current !== ticket) return;
                            onFile(value, file.name);
                            onError("");
                          })
                          .catch((cause) => {
                            if (fileRead.current === ticket)
                              onError(errorMessage(cause));
                          })
                          .finally(() => {
                            if (fileRead.current === ticket) setReading(false);
                          });
                      }}
                    />
                    <button
                      type="button"
                      onClick={() => input.current?.click()}
                      className="group flex w-full items-center gap-4 rounded-xl border border-dashed border-border bg-surface-well p-5 text-left transition-control hover:border-status-info/50 hover:bg-state-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-status-info/30 disabled:pointer-events-none"
                    >
                      <span className="flex size-11 shrink-0 items-center justify-center rounded-xl border border-border-subtle bg-surface-base text-status-info">
                        {fileName ? (
                          <FileCheck2 className="size-5" />
                        ) : (
                          <FileUp className="size-5" />
                        )}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-body-13-medium text-text-primary">
                          {reading
                            ? "Reading file…"
                            : fileName || "Choose a collection or environment"}
                        </span>
                        <span className="mt-1 block text-caption-11 text-text-tertiary">
                          {fileName
                            ? "Click to choose a different file"
                            : "Browse files, or paste their contents below"}
                        </span>
                      </span>
                      <ArrowRight className="size-4 shrink-0 text-text-tertiary" />
                    </button>
                    <div className="grid gap-4 sm:grid-cols-2">
                      <label className="flex min-w-0 flex-col gap-2 text-caption-11-medium text-text-secondary">
                        File format
                        <NativeSelect
                          className="w-full"
                          value={format}
                          onChange={(e) => onFormatChange(e.target.value)}
                          aria-label="Format"
                        >
                          <NativeSelectOption value="">
                            Detect automatically
                          </NativeSelectOption>
                          {formats.map((f) => (
                            <NativeSelectOption key={f.id} value={f.id}>
                              {f.label}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      </label>
                      <label className="flex min-w-0 flex-col gap-2 text-caption-11-medium text-text-secondary">
                        Environment tag
                        <NativeSelect
                          className="w-full"
                          value={tag}
                          onChange={(e) => onTagChange(e.target.value)}
                          aria-label="Environment tag"
                        >
                          <NativeSelectOption value="">
                            Use tag from file
                          </NativeSelectOption>
                          {tags.map((t) => (
                            <NativeSelectOption key={t.id} value={t.name}>
                              {t.name}
                              {t.requiresConfirmation
                                ? " — asks before every send"
                                : ""}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      </label>
                    </div>
                  </>
                ) : (
                  <p className="text-body-13 text-text-secondary">
                    Paste a command from your terminal or your browser’s “Copy
                    as cURL”. You can review and edit it before sending.
                  </p>
                )}
                <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface-well focus-within:border-status-info/50 focus-within:ring-2 focus-within:ring-inset focus-within:ring-status-info/20">
                  <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2.5">
                    <label
                      htmlFor={textId}
                      className="text-caption-11-medium text-text-secondary"
                    >
                      {fileMode ? "File contents" : "cURL command"}
                    </label>
                    <span className="text-caption-11 text-text-tertiary">
                      {fileMode ? "JSON / YAML / text" : "Opens as a draft"}
                    </span>
                  </div>
                  <Textarea
                    id={textId}
                    value={text}
                    onChange={(e) => onTextChange(e.target.value)}
                    spellCheck={false}
                    placeholder={
                      fileMode
                        ? "Paste your collection or environment here…"
                        : "curl 'https://api.example.com/v1/users' \\\n  -H 'Accept: application/json'"
                    }
                    className="h-40 min-h-32 resize-y rounded-none border-0 bg-transparent font-mono text-code-12 leading-relaxed shadow-none focus-visible:ring-0 [field-sizing:fixed]"
                    aria-label="Import text"
                  />
                </div>
              </fieldset>
            </TabsContent>
          </Tabs>
          <p className="mt-4 flex items-start gap-2 text-caption-11 text-text-tertiary">
            <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
            <span>Nothing is sent. Scripts in imported files are skipped.</span>
          </p>
        </div>
          {error ? (
            <p
              role="alert"
              className="mx-6 mb-3 shrink-0 rounded-lg bg-status-danger/10 p-3 text-caption-11 text-status-danger"
            >
              {error}
            </p>
          ) : null}
        <DialogFooter className="m-0 shrink-0 flex-row items-center justify-end bg-surface-well/50 px-6 py-4">
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            className="min-w-32"
            onClick={onSubmit}
            disabled={!text.trim() || busy || reading}
          >
            {busy ? (
              <Loader2 className="motion-safe:animate-spin" />
            ) : (
              <ArrowRight />
            )}
            {busy ? "Importing…" : fileMode ? "Import file" : "Open request"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
