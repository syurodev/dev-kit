import type { ClipboardEvent, ReactNode } from "react";
import {
  Folder,
  Globe,
  Loader2,
  Save,
  Send,
  SlidersHorizontal,
  Terminal,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@/components/ui/native-select";
import { cn } from "@/lib/utils";
import { shortcut } from "@/lib/shortcut";
import { METHODS } from "./requestModel";
import { methodTextClass } from "@/lib/httpMethod";

/** One document's identity and send target, followed by its primary action. */
export function RequestToolbar({
  name,
  onNameChange,
  dirty,
  folders,
  folderId,
  onFolderChange,
  environments,
  environmentId,
  onEnvironmentChange,
  environmentTag,
  gated,
  onManageEnvironment,
  url,
  onUrlChange,
  onUrlFocus,
  onUrlBlur,
  onUrlPaste,
  method,
  onMethodChange,
  sending,
  onSend,
  onCancel,
  onSave,
  onCurl,
}: {
  name: string;
  onNameChange: (name: string) => void;
  dirty: boolean;
  folders: { id: string; label: string }[];
  folderId: string;
  onFolderChange: (id: string) => void;
  environments: { id: string; label: string }[];
  environmentId: string;
  onEnvironmentChange: (id: string) => void;
  environmentTag?: ReactNode;
  gated: boolean;
  onManageEnvironment: () => void;
  url: string;
  onUrlChange: (url: string) => void;
  onUrlFocus?: () => void;
  onUrlBlur?: () => void;
  onUrlPaste?: (event: ClipboardEvent<HTMLInputElement>) => void;
  method: string;
  onMethodChange: (method: string) => void;
  sending: boolean;
  onSend: () => void;
  onCancel: () => void;
  onSave: () => void;
  onCurl: () => void;
}) {
  return (
    <div className="shrink-0 space-y-3 border-b border-border-subtle px-4 pt-3 pb-4">
      <header className="flex flex-wrap items-center gap-2">
        <input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          aria-label="Request name"
          spellCheck={false}
          className="h-8 min-w-28 flex-1 truncate rounded-md bg-transparent px-2 text-title-14 text-text-primary outline-none transition-control hover:bg-state-hover focus:bg-surface-well focus-visible:ring-2 focus-visible:ring-border-focus/50"
        />
        <span
          className={cn(
            "size-1.5 shrink-0 rounded-full bg-status-warning",
            !dirty && "invisible",
          )}
          title="Unsaved changes"
        />
        <div className="flex min-w-0 items-center gap-1.5 text-text-tertiary">
          <Folder className="size-3.5 shrink-0" aria-hidden />
          <NativeSelect
            size="sm"
            value={folderId}
            aria-label="Folder"
            className="max-w-48"
            onChange={(e) => onFolderChange(e.target.value)}
          >
            <NativeSelectOption value="">No collection</NativeSelectOption>
            {folders.map((f) => (
              <NativeSelectOption key={f.id} value={f.id}>
                {f.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={!dirty || sending}
          onClick={onSave}
          title={`Save (${shortcut("S")})`}
        >
          <Save />
          Save
        </Button>
      </header>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption-11 text-text-secondary">
        <Globe className="size-3.5 text-text-tertiary" aria-hidden />
        <span>Send to</span>
        <NativeSelect
          size="sm"
          value={environmentId}
          aria-label="Environment"
          className="max-w-52"
          onChange={(e) => onEnvironmentChange(e.target.value)}
          disabled={sending}
        >
          {!environmentId ? <NativeSelectOption value="">Choose environment</NativeSelectOption> : null}
          {environments.map((e) => (
            <NativeSelectOption key={e.id} value={e.id}>
              {e.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {environmentTag}
        {environmentId && gated ? (
          <span className="text-text-tertiary">Approval required</span>
        ) : null}
        <Button
          size="icon-xs"
          variant="ghost"
          onClick={onManageEnvironment}
          aria-label={
            environmentId ? "Edit environment variables" : "Create environment"
          }
          title={
            environmentId ? "Edit environment variables" : "Create environment"
          }
        >
          <SlidersHorizontal />
        </Button>
        <div className="flex-1" />
        <Button
          size="xs"
          variant="ghost"
          onClick={onCurl}
          title="Copy as cURL — credentials remain shell variables"
        >
          <Terminal />
          Copy cURL
        </Button>
      </div>
      <div className="flex items-stretch gap-2">
        <div className="flex min-w-0 flex-1 items-center overflow-hidden rounded-lg border border-border-subtle bg-surface-well transition-control focus-within:border-border-focus/50 focus-within:ring-2 focus-within:ring-border-focus/50">
          <NativeSelect
            value={method}
            aria-label="Method"
            onChange={(e) => onMethodChange(e.target.value)}
            className={cn(
              "w-26 shrink-0 font-mono [&_select]:rounded-none [&_select]:border-0 [&_select]:border-r [&_select]:border-border-subtle [&_select]:bg-transparent [&_select]:shadow-none [&_select]:text-inherit",
              methodTextClass(method),
            )}
          >
            {METHODS.map((m) => (
              <NativeSelectOption key={m} value={m}>
                {m}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <input
            value={url}
            onChange={(e) => onUrlChange(e.target.value)}
            onFocus={onUrlFocus}
            onBlur={onUrlBlur}
            onPaste={onUrlPaste}
            placeholder="{{baseUrl}}/users/:id"
            aria-label="URL"
            spellCheck={false}
            className="h-10 min-w-0 flex-1 bg-transparent px-3 font-mono text-code-12 text-text-primary outline-none placeholder:text-text-tertiary"
          />
        </div>
        <Button
          className="h-10 min-w-24 rounded-lg"
          variant={sending ? "outline" : "default"}
          onClick={sending ? onCancel : onSend}
          disabled={!sending && (!url.trim() || !environmentId)}
          title={`${sending ? "Cancel request" : "Send request"} (${shortcut("Enter")})`}
        >
          {sending ? (
            <Loader2 className="motion-safe:animate-spin" />
          ) : (
            <Send />
          )}
          {sending ? "Cancel" : "Send"}
        </Button>
      </div>
    </div>
  );
}
