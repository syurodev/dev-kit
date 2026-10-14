import { useMemo, useState } from "react";
import { Copy, Send, X } from "lucide-react";
import { toast } from "sonner";
import type { ApiResponse, KV } from "@/bridge/apiclient";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { detectLanguage, languageForContentType } from "@/components/code/languages";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { EmptyState } from "@/components/ui/empty-state";
import { Button } from "@/components/ui/button";
import { AnimatedNumber } from "@/components/ui/animated-number";
import { Tabs, TabsContent, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { formatBytes, prettyBody, statusTone } from "./requestModel";
import { RequestHistory } from "./RequestHistory";

export function ResponsePane({
  response,
  sending,
  error,
  curl,
  onCloseCurl,
  requestId,
  sends,
}: {
  response: ApiResponse | null;
  sending: boolean;
  error: string;
  curl: string;
  onCloseCurl: () => void;
  requestId: string;
  sends: number;
}) {
  const [tab, setTab] = useState("body");
  const [raw, setRaw] = useState(false);
  const contentType = Object.entries(response?.headers ?? {}).find(([name]) => name.toLowerCase() === "content-type")?.[1];
  const language = useMemo(() => response && tab === "body" && !raw ? (languageForContentType(contentType) ?? detectLanguage(response.body)) : undefined, [response, contentType, tab, raw]);
  const extension = useLanguageExtension(raw ? undefined : language);
  const shown = useMemo(() => (response && tab === "body" ? (raw ? response.body : prettyBody(response.body, language)) : ""), [response, raw, language, tab]);
  const headerRows: KV[] = response?.headerList ?? Object.entries(response?.headers ?? {}).map(([key, value]) => ({ key, value }));
  return (
    <div className="flex min-h-0 flex-1 flex-col px-4 pt-3">
      {curl ? (
        <div className="relative mb-2">
          <Textarea readOnly value={curl} rows={5} aria-label="cURL command" className="font-mono text-code-11" />
          <button
            type="button"
            onClick={onCloseCurl}
            aria-label="Close the cURL command"
            className="absolute top-1.5 right-1.5 rounded p-0.5 text-text-tertiary transition-control hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-border-focus/50"
          >
            <X className="size-(--icon-12)" />
          </button>
        </div>
      ) : null}
      <div className="flex min-h-9 flex-wrap items-center gap-x-3 gap-y-1 pb-1 text-caption-11">
        <span className="text-label-11-caps text-text-tertiary">Response</span>
        {sending ? <span role="status" className="text-text-secondary">Sending…</span> : null}
        {response ? (
          <>
            {sending || error ? <span className="text-text-tertiary">Previous response</span> : null}
            <span className={cn("rounded-md bg-state-soft px-2 py-1 font-mono text-code-12-medium", statusTone(response.status))}>
              {response.statusText || response.status}
            </span>
            <AnimatedNumber value={response.durationMs} suffix=" ms" className="min-w-14 text-text-secondary" />
            <span className="min-w-14 tabular-nums text-text-secondary">{formatBytes(response.bytes)}</span>
            {response.truncated ? <span className="text-status-warning">truncated</span> : null}
          </>
        ) : null}
        <div className="flex-1" />
        {response ? (
          <Button size="xs" variant="ghost" onClick={() => void copyText(response.body).then(() => toast.success("Body copied")).catch((cause) => toast.error("Could not copy body", { description: errorMessage(cause) }))}>
            <Copy /> Copy body
          </Button>
        ) : null}
      </div>
      {error && !sending ? <p role="alert" className="mb-2 rounded-md border border-status-danger/20 bg-surface-well p-2 text-caption-11 text-status-danger">{error}</p> : null}
      {response?.redirectBlocked ? (
        <p className="pb-2 text-caption-11 text-status-warning">
          Redirect to {response.redirectBlocked} was not followed: it leaves the origin, and the credential would have gone with it.
        </p>
      ) : null}

      <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-2">
        <TabsList variant="line" className="max-w-full w-fit overflow-x-auto">
          <TabsTrigger value="body">Body</TabsTrigger>
          <TabsTrigger value="headers">
            Headers {headerRows.length ? <TabsCount>{headerRows.length}</TabsCount> : null}
          </TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-auto overscroll-contain pb-3">
          <TabsContent value="body" className="flex h-full min-h-0 flex-col gap-1">
            {response ? (
              <>
                <div className="flex items-center gap-1">
                  <Button size="xs" variant={raw ? "ghost" : "secondary"} aria-pressed={!raw} onClick={() => setRaw(false)}>
                    Pretty
                  </Button>
                  <Button size="xs" variant={raw ? "secondary" : "ghost"} aria-pressed={raw} onClick={() => setRaw(true)}>
                    Raw
                  </Button>
                </div>
                <div className="relative min-h-40 flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
                  <CodeEditor value={shown} onChange={() => undefined} readOnly extensions={extension} wrap="auto" />
                </div>
              </>
            ) : (
              <EmptyState size="rail" icon={Send} busy={sending} title={sending ? "Waiting for response" : "Ready when you are"} body={sending ? "The response will appear here. You can cancel from the request bar." : "Choose an environment and send your request to inspect its body, headers and timing."} />
            )}
          </TabsContent>
          <TabsContent value="headers">
            {headerRows.length ? (
              <dl className="grid grid-cols-5 gap-x-3 text-code-12">
                {headerRows.map((row, index) => (
                  <div key={`${row.key}-${index}`} className="col-span-5 grid grid-cols-subgrid border-b border-border-subtle py-1 last:border-b-0">
                    <dt className="col-span-2 font-mono break-all text-text-secondary">{row.key}</dt>
                    <dd className="col-span-3 font-mono break-all text-text-primary">{row.value}</dd>
                  </div>
                ))}
              </dl>
            ) : (
              <p className="pt-4 text-center text-caption-11 text-text-tertiary">No response yet.</p>
            )}
          </TabsContent>
          <TabsContent value="history">
            {requestId ? (
              <RequestHistory key={`${requestId}-${sends}`} requestID={requestId} />
            ) : (
              <p className="pt-4 text-center text-caption-11 text-text-tertiary">
                Save the request to keep a history of what this machine sent.
              </p>
            )}
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}
