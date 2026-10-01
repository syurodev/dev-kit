import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent } from "react";
import { toast } from "sonner";
import { Copy, Loader2, Save, Send, Terminal, Wand2, X } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import {
  BODY_MODES,
  cancelSend,
  curlFor,
  draftFromEndpoint,
  getRequest,
  looksLikeCurl,
  parseCurl,
  resolveUrl,
  saveRequest,
  sendDraft,
  variablesFor,
  type ApiResponse,
  type BodyMode,
  type KV,
  type RequestInput,
  type ResolvedVar,
} from "@/bridge/apiclient";
import { copyText } from "@/bridge/clipboard";
import { errorMessage, isCancelError } from "@/bridge/errors";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { detectLanguage, languageForContentType } from "@/components/code/languages";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { moduleTabs, tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { activeEnvironment, tagAsks, useApiClientStore, type EditorState } from "./apiclientStore";
import { EnvTagChip } from "./ApiClientSidebar";
import { AuthPanel } from "./AuthPanel";
import { folderPath, foldersByPath } from "./collections";
import { closeTabByParams, MODULE, openRequest, REQUEST_VIEW } from "./documents";
import { KeyValueTable } from "./KeyValueTable";
import { RequestHistory } from "./RequestHistory";
import {
  activeRows,
  emptyRequest,
  formatBytes,
  METHODS,
  methodTone,
  parseUrlBar,
  prettyBody,
  referencedVars,
  sameRequest,
  statusTone,
  syncPathVars,
  urlBarText,
} from "./requestModel";

/** In-flight sends by editor key, so Cancel reaches the bridge call. */
const inflight = new Map<string, { cancel: () => void }>();

function newSendId(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * The editor key a tab's params name: one saved request, one new tab, one
 * draft. A tab with none is the module's home — what "Open request" on the
 * empty module opens — and it is a blank request ready to type into rather
 * than a page saying there is nothing here.
 */
const HOME_KEY = "new:home";
function editorKey(params?: Record<string, unknown>): string {
  if (typeof params?.requestId === "string" && params.requestId) return `request:${params.requestId}`;
  if (typeof params?.newId === "string" && params.newId) return `new:${params.newId}`;
  if (typeof params?.endpointId === "string" && params.endpointId) return `endpoint:${params.endpointId}`;
  return HOME_KEY;
}

function freshEditor(request: RequestInput, baseline: RequestInput | null, note = ""): EditorState {
  return { request, baseline, response: null, sending: false, sendId: "", error: "", note, sends: 0 };
}

/** A saved request as the editor holds it: lists never null, a body mode always set. */
function forEditor(request: RequestInput): RequestInput {
  return {
    ...emptyRequest(request.collectionId),
    ...request,
    query: request.query ?? [],
    headers: request.headers ?? [],
    pathVars: syncPathVars(request.url, request.pathVars),
    form: request.form ?? [],
    bodyMode: request.bodyMode || "none",
    body: request.body ?? "",
    auth: request.auth ?? { kind: "", accountId: "" },
  };
}

/**
 * Compose, send, save — one request per tab, Postman's shape.
 *
 * The tab edits a working copy; nothing is written until Save (⌘S), and Send
 * (⌘↵) sends what is on screen, saved or not, against the module's active
 * environment. Auth is an account binding: this view never holds a
 * credential, which is also why Copy as cURL is safe to paste.
 */
export function ApiRequestView({ params }: { params?: Record<string, unknown> } = {}) {
  const key = editorKey(params);
  const editor = useApiClientStore((s) => (key ? s.editors[key] : undefined));
  const [loadError, setLoadError] = useState("");

  useEffect(() => {
    if (!key || useApiClientStore.getState().editors[key]) return;
    const { setEditor, takePending } = useApiClientStore.getState();
    setLoadError("");
    if (typeof params?.requestId === "string" && params.requestId) {
      getRequest(params.requestId)
        .then((saved) => {
          const request = forEditor(saved);
          setEditor(key, freshEditor(request, request));
        })
        .catch((e) => setLoadError(errorMessage(e)));
    } else if (typeof params?.newId === "string" && params.newId) {
      setEditor(key, freshEditor(forEditor(takePending(params.newId) ?? emptyRequest()), null));
    } else if (typeof params?.endpointId === "string" && params.endpointId) {
      // An endpoint id rather than a draft: a tab reopened after a restart
      // asks for a draft of the CURRENT index.
      draftFromEndpoint(params.endpointId)
        .then((draft) => setEditor(key, freshEditor(forEditor({ ...draft, id: "" }), null, draft.bodyInferred ? "" : draft.note)))
        .catch((e) => setLoadError(errorMessage(e)));
    } else {
      setEditor(key, freshEditor(emptyRequest(), null));
    }
  }, [key, params?.requestId, params?.newId, params?.endpointId]);

  if (loadError) {
    return <EmptyState icon={Send} title="This request could not be opened" body={loadError} />;
  }
  if (!editor) return null;
  return <RequestEditor editorKeyValue={key} editor={editor} params={params} />;
}

function RequestEditor({
  editorKeyValue: key,
  editor,
  params,
}: {
  editorKeyValue: string;
  editor: EditorState;
  params?: Record<string, unknown>;
}) {
  const { collections, environments, globals, envTags, activeEnvironmentId } = useApiClientStore(
    useShallow((s) => ({
      collections: s.collections,
      environments: s.environments,
      globals: s.globals,
      envTags: s.envTags,
      activeEnvironmentId: s.activeEnvironmentId,
    })),
  );
  const store = useApiClientStore.getState();
  const environment = activeEnvironment({ environments, activeEnvironmentId });
  const { request, response, sending } = editor;
  const dirty = !editor.baseline || !sameRequest(editor.baseline, request);
  const [tab, setTab] = useState("params");
  const [urlDraft, setUrlDraft] = useState<string | null>(null);
  const [curl, setCurl] = useState("");
  const [resolved, setResolved] = useState<ResolvedVar[] | null>(null);

  useEffect(() => {
    if (!collections) void store.refresh();
  }, [collections, store]);

  // What every {{name}} resolves to here: globals, the folder chain, the
  // environment. Re-read when any of them may have changed.
  useEffect(() => {
    let cancelled = false;
    variablesFor(request.collectionId, environment?.id ?? "")
      .then((vars) => !cancelled && setResolved(vars))
      .catch(() => !cancelled && setResolved(null));
    return () => {
      cancelled = true;
    };
  }, [request.collectionId, environment?.id, collections, environments, globals]);

  const plainVars = useMemo(() => {
    const out: Record<string, string> = {};
    for (const v of resolved ?? []) if (!v.secret) out[v.name] = v.value;
    return out;
  }, [resolved]);
  const missing = useMemo(() => {
    if (!resolved) return [];
    const known = new Set(resolved.map((v) => v.name));
    return referencedVars(request).filter((name) => !known.has(name));
  }, [resolved, request]);

  const update = useCallback(
    (patch: Partial<RequestInput>) => {
      const current = useApiClientStore.getState().editors[key];
      if (!current) return;
      useApiClientStore.getState().patchEditor(key, { request: { ...current.request, ...patch } });
    },
    [key],
  );

  function setUrlText(text: string) {
    const parsed = parseUrlBar(text, request.query);
    update({ url: parsed.url, query: parsed.query, pathVars: syncPathVars(parsed.url, request.pathVars) });
  }

  async function adoptCurl(text: string) {
    try {
      const parsed = await parseCurl(text);
      const untitled = !request.name || request.name === emptyRequest().name;
      update({
        method: parsed.method,
        url: parsed.url,
        query: parsed.query ?? [],
        headers: parsed.headers ?? [],
        pathVars: syncPathVars(parsed.url, []),
        bodyMode: parsed.bodyMode || "none",
        body: parsed.body ?? "",
        form: parsed.form ?? [],
        name: untitled ? parsed.name : request.name,
      });
      toast.success("Read the curl command into this request");
    } catch (e) {
      toast.error("Could not read that curl command", { description: errorMessage(e) });
    }
  }

  function send() {
    if (sending) return;
    if (!environment) {
      store.patchEditor(key, { error: "Pick an environment first — its tag decides whether sending asks before it goes." });
      return;
    }
    const sendId = newSendId();
    store.patchEditor(key, { sending: true, sendId, error: "" });
    const call = sendDraft(request, environment.id, sendId);
    inflight.set(key, call);
    call
      .then((result) => {
        const current = useApiClientStore.getState().editors[key];
        store.patchEditor(key, { response: result, sending: false, sends: (current?.sends ?? 0) + 1 });
      })
      .catch((e) => {
        const current = useApiClientStore.getState().editors[key];
        store.patchEditor(key, {
          sending: false,
          error: isCancelError(e) ? "Cancelled." : errorMessage(e),
          sends: (current?.sends ?? 0) + 1,
        });
      })
      .finally(() => inflight.delete(key));
  }

  function cancel() {
    inflight.get(key)?.cancel();
    if (editor.sendId) void cancelSend(editor.sendId);
  }

  async function save() {
    try {
      const saved = forEditor(await saveRequest(request));
      if (request.id) {
        store.patchEditor(key, { request: saved, baseline: saved });
      } else {
        // A new request becomes a saved one: its editor moves to the saved
        // request's key, response and all, and the tab follows it.
        store.setEditor(`request:${saved.id}`, { ...editor, request: saved, baseline: saved, note: "" });
        openRequest(saved.id, saved.name);
        if (typeof params?.newId === "string") closeTabByParams(REQUEST_VIEW, { newId: params.newId });
        else if (typeof params?.endpointId === "string") closeTabByParams(REQUEST_VIEW, { endpointId: params.endpointId });
        else closeTabByParams(REQUEST_VIEW, {});
        store.dropEditor(key);
      }
      await store.refresh();
      toast.success(`Saved ${saved.name}`);
    } catch (e) {
      toast.error("Could not save the request", { description: errorMessage(e) });
    }
  }

  /** ⌘↵ sends, ⌘S saves. True when the key was one of them. */
  function shortcut(event: Pick<KeyboardEvent, "metaKey" | "ctrlKey" | "key" | "preventDefault">): boolean {
    const mod = event.metaKey || event.ctrlKey;
    if (mod && event.key === "Enter") {
      event.preventDefault();
      send();
      return true;
    }
    if (mod && (event.key === "s" || event.key === "S")) {
      event.preventDefault();
      void save();
      return true;
    }
    return false;
  }

  // Inside the editor the keys are caught on the way down, before CodeMirror
  // turns ⌘↵ into a blank line.
  function onKeyDownCapture(event: KeyboardEvent) {
    if (shortcut(event)) event.stopPropagation();
  }

  // Outside it — the request just picked in the rail, focus still there —
  // they still mean "this request", as long as this is the tab in front.
  const shortcutRef = useRef(shortcut);
  shortcutRef.current = shortcut;
  const inFront = useWorkbenchTabsStore(
    (s) => s.activeModuleId === MODULE && moduleTabs(s.modules, MODULE).activeTabId === tabKey(MODULE, REQUEST_VIEW, params),
  );
  useEffect(() => {
    if (!inFront) return;
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!event.defaultPrevented && !event.isComposing) shortcutRef.current(event);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [inFront]);

  function onUrlPaste(event: ClipboardEvent<HTMLInputElement>) {
    const text = event.clipboardData.getData("text");
    if (looksLikeCurl(text)) {
      event.preventDefault();
      setUrlDraft(null);
      void adoptCurl(text);
    }
  }

  const queryCount = activeRows(request.query).length;
  const headerCount = activeRows(request.headers).length;
  const gated = environment ? tagAsks(envTags, environment.env) : false;
  const folders = foldersByPath(collections ?? []);

  return (
    <div className="flex h-full min-h-0 flex-col" onKeyDownCapture={onKeyDownCapture}>
      {/* Name, folder, environment, save. */}
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-1.5">
        <input
          value={request.name}
          onChange={(event) => update({ name: event.target.value })}
          aria-label="Request name"
          spellCheck={false}
          className="min-w-0 flex-1 truncate rounded-md bg-transparent px-1 text-body-13-medium text-text-primary outline-none hover:bg-surface-raised focus:bg-surface-well"
        />
        {dirty ? (
          <span className="size-1.5 shrink-0 rounded-full bg-status-warning" title={editor.baseline ? "Unsaved changes" : "Not saved yet"} />
        ) : null}
        <NativeSelect
          size="sm"
          value={request.collectionId}
          aria-label="Folder"
          onChange={(event) => update({ collectionId: event.target.value })}
          className="max-w-48"
        >
          <NativeSelectOption value="">No collection</NativeSelectOption>
          {folders.map((folder) => (
            <NativeSelectOption key={folder.id} value={folder.id}>
              {folderPath(collections ?? [], folder.id)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <div className="flex shrink-0 items-center gap-1">
          <NativeSelect
            size="sm"
            value={environment?.id ?? ""}
            aria-label="Environment"
            onChange={(event) => store.setActiveEnvironment(event.target.value)}
            className="max-w-48"
          >
            {!environment ? <NativeSelectOption value="">No environment</NativeSelectOption> : null}
            {(environments ?? []).map((candidate) => (
              <NativeSelectOption key={candidate.id} value={candidate.id}>
                {candidate.name} ({candidate.env})
              </NativeSelectOption>
            ))}
          </NativeSelect>
          {environment ? <EnvTagChip tag={environment.env} /> : null}
        </div>
        <Button
          size="xs"
          variant="ghost"
          title="Copy as cURL — the credential stays a shell variable"
          onClick={() => {
            curlFor(request, environment?.id ?? "")
              .then((text) => {
                setCurl(text);
                return copyText(text).then(() => toast.success("cURL copied"));
              })
              .catch((e) => toast.error("Could not build the cURL command", { description: errorMessage(e) }));
          }}
        >
          <Terminal /> cURL
        </Button>
        <Button size="xs" variant={dirty ? "default" : "ghost"} disabled={!dirty} onClick={() => void save()} title="Save (⌘S)">
          <Save /> Save
        </Button>
      </header>

      {/* Method, address, send. */}
      <div className="flex items-center gap-2 px-3 py-2">
        <NativeSelect
          size="sm"
          value={request.method}
          aria-label="Method"
          onChange={(event) => update({ method: event.target.value })}
          className={cn("w-28 font-mono", methodTone(request.method))}
        >
          {METHODS.map((method) => (
            <NativeSelectOption key={method} value={method}>
              {method}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <input
          value={urlDraft ?? urlBarText(request.url, request.query)}
          onFocus={() => setUrlDraft(urlBarText(request.url, request.query))}
          onBlur={() => setUrlDraft(null)}
          onChange={(event) => {
            setUrlDraft(event.target.value);
            setUrlText(event.target.value);
          }}
          onPaste={onUrlPaste}
          placeholder="{{baseUrl}}/users/:id — or paste a curl command"
          aria-label="URL"
          spellCheck={false}
          className="h-(--control-height-sm) min-w-0 flex-1 rounded-md border border-border-subtle bg-surface-well px-2.5 font-mono text-code-12 text-text-primary outline-none placeholder:text-text-tertiary focus:border-border-focus"
        />
        {sending ? (
          <Button size="sm" variant="outline" onClick={cancel}>
            <Loader2 className="animate-spin" /> Cancel
          </Button>
        ) : (
          <Button
            size="sm"
            onClick={send}
            disabled={!request.url.trim()}
            title={gated ? `Send (⌘↵) — ${environment?.env} asks before every send` : "Send (⌘↵)"}
          >
            <Send /> Send
          </Button>
        )}
      </div>

      {missing.length > 0 || !environment || editor.note || request.originRef ? (
        <div className="flex flex-col gap-0.5 px-3 pb-2 text-caption-11">
          {!environment ? (
            <p className="text-status-warning">
              No environment yet. Create one under Environments in the sidebar — its tag decides whether a send asks first.
            </p>
          ) : null}
          {missing.length > 0 ? (
            <p className="text-status-warning">
              No value in Globals, this collection or {environment?.name ?? "the environment"}: {missing.map((name) => `{{${name}}}`).join(", ")}
            </p>
          ) : null}
          {editor.note ? <p className="text-text-tertiary">{editor.note}</p> : null}
          {request.originRef ? <p className="text-text-tertiary">Drafted from a Code Graph endpoint.</p> : null}
        </div>
      ) : null}

      <ResizablePanelGroup orientation="vertical" className="min-h-0 flex-1">
        <ResizablePanel id="request" minSize="15" defaultSize="45" className="flex min-h-0 flex-col">
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-2 px-3">
            <TabsList variant="line" className="w-fit">
              <TabsTrigger value="params">
                Params {queryCount ? <TabsCount>{queryCount}</TabsCount> : null}
              </TabsTrigger>
              <TabsTrigger value="headers">
                Headers {headerCount ? <TabsCount>{headerCount}</TabsCount> : null}
              </TabsTrigger>
              <TabsTrigger value="body">
                Body {request.bodyMode && request.bodyMode !== "none" ? <TabsCount>{BODY_MODES.find((m) => m.id === request.bodyMode)?.label}</TabsCount> : null}
              </TabsTrigger>
              <TabsTrigger value="auth">
                Auth {request.auth.kind && request.auth.accountId ? <TabsCount>✓</TabsCount> : null}
              </TabsTrigger>
            </TabsList>
            <div className="min-h-0 flex-1 overflow-auto pb-3">
              <TabsContent value="params" className="flex flex-col gap-3">
                <KeyValueTable rows={request.query ?? []} onChange={(rows) => update({ query: rows })} keyPlaceholder="Parameter" />
                {(request.pathVars ?? []).length > 0 ? (
                  <section className="flex flex-col gap-1">
                    <h3 className="text-label-11-caps text-text-tertiary">Path variables</h3>
                    <KeyValueTable
                      rows={request.pathVars ?? []}
                      onChange={(rows) => update({ pathVars: rows })}
                      fixedKeys
                      toggleable={false}
                      valuePlaceholder="Value"
                    />
                  </section>
                ) : null}
              </TabsContent>
              <TabsContent value="headers">
                <KeyValueTable rows={request.headers ?? []} onChange={(rows) => update({ headers: rows })} keyPlaceholder="Header" />
                <p className="pt-2 text-caption-11 text-text-tertiary">
                  Content-Type follows the body unless set here. Credentials belong in Auth, not in a header row.
                </p>
              </TabsContent>
              <TabsContent value="body" className="flex h-full min-h-0 flex-col gap-2">
                <BodyEditor request={request} update={update} />
              </TabsContent>
              <TabsContent value="auth">
                <AuthPanel
                  auth={request.auth}
                  onChange={(auth) => update({ auth })}
                  resolvedUrl={resolveUrl(request.url, plainVars)}
                  env={environment?.env ?? ""}
                />
              </TabsContent>
            </div>
          </Tabs>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="response" minSize="15" className="flex min-h-0 flex-col">
          <ResponsePane
            response={response}
            sending={sending}
            error={editor.error}
            curl={curl}
            onCloseCurl={() => setCurl("")}
            requestId={request.id}
            sends={editor.sends}
          />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

function BodyEditor({ request, update }: { request: RequestInput; update: (patch: Partial<RequestInput>) => void }) {
  const mode = (request.bodyMode || "none") as BodyMode;
  const language = mode === "json" ? "json" : mode === "xml" ? "xml" : undefined;
  const extension = useLanguageExtension(language);
  const extensions = useMemo(() => extension, [extension]);

  return (
    <>
      <div className="flex items-center gap-2">
        <NativeSelect size="sm" value={mode} aria-label="Body type" onChange={(event) => update({ bodyMode: event.target.value as BodyMode })}>
          {BODY_MODES.map((option) => (
            <NativeSelectOption key={option.id} value={option.id}>
              {option.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        {mode === "json" ? (
          <Button
            size="xs"
            variant="ghost"
            title="Indent the JSON"
            onClick={() => {
              const pretty = prettyBody(request.body ?? "", "json");
              if (pretty === request.body) toast.info("Nothing to change — or it is not valid JSON yet.");
              update({ body: pretty });
            }}
          >
            <Wand2 /> Beautify
          </Button>
        ) : null}
      </div>
      {mode === "none" ? (
        <p className="text-caption-11 text-text-tertiary">This request sends no body.</p>
      ) : mode === "urlencoded" || mode === "multipart" ? (
        <>
          <KeyValueTable rows={request.form ?? []} onChange={(rows) => update({ form: rows })} keyPlaceholder="Field" />
          {mode === "multipart" ? (
            <p className="text-caption-11 text-text-tertiary">Text fields only — file uploads are not supported yet.</p>
          ) : null}
        </>
      ) : (
        <div className="relative min-h-40 flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
          <CodeEditor value={request.body ?? ""} onChange={(body) => update({ body })} extensions={extensions} wrap="auto" />
        </div>
      )}
    </>
  );
}

function ResponsePane({
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
  const language = response ? (languageForContentType(contentType) ?? detectLanguage(response.body)) : undefined;
  const extension = useLanguageExtension(raw ? undefined : language);
  const shown = useMemo(() => (response ? (raw ? response.body : prettyBody(response.body, language)) : ""), [response, raw, language]);
  const headerRows: KV[] = response?.headerList ?? Object.entries(response?.headers ?? {}).map(([key, value]) => ({ key, value }));
  return (
    <div className="flex min-h-0 flex-1 flex-col px-3 pt-2">
      {curl ? (
        <div className="relative mb-2">
          <Textarea readOnly value={curl} rows={5} aria-label="cURL command" className="font-mono text-code-11" />
          <button
            type="button"
            onClick={onCloseCurl}
            aria-label="Close the cURL command"
            className="absolute top-1.5 right-1.5 rounded p-0.5 text-text-tertiary hover:text-text-primary"
          >
            <X className="size-(--icon-12)" />
          </button>
        </div>
      ) : null}
      <div className="flex items-center gap-3 pb-1 text-caption-11">
        <span className="text-label-11-caps text-text-tertiary">Response</span>
        {sending ? <span className="text-text-secondary">Sending…</span> : null}
        {response && !sending ? (
          <>
            <span className={cn("font-mono text-code-12-medium", statusTone(response.status))}>
              {response.statusText || response.status}
            </span>
            <span className="text-text-secondary">{response.durationMs} ms</span>
            <span className="text-text-secondary">{formatBytes(response.bytes)}</span>
            {response.truncated ? <span className="text-status-warning">truncated</span> : null}
          </>
        ) : null}
        <div className="flex-1" />
        {response ? (
          <Button size="xs" variant="ghost" onClick={() => void copyText(response.body).then(() => toast.success("Body copied"))}>
            <Copy /> Copy body
          </Button>
        ) : null}
      </div>
      {error && !sending ? <p className="pb-2 text-caption-11 text-status-danger">{error}</p> : null}
      {response?.redirectBlocked ? (
        <p className="pb-2 text-caption-11 text-status-warning">
          Redirect to {response.redirectBlocked} was not followed: it leaves the origin, and the credential would have gone with it.
        </p>
      ) : null}

      <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-2">
        <TabsList variant="line" className="w-fit">
          <TabsTrigger value="body">Body</TabsTrigger>
          <TabsTrigger value="headers">
            Headers {headerRows.length ? <TabsCount>{headerRows.length}</TabsCount> : null}
          </TabsTrigger>
          <TabsTrigger value="history">History</TabsTrigger>
        </TabsList>
        <div className="min-h-0 flex-1 overflow-auto pb-3">
          <TabsContent value="body" className="flex h-full min-h-0 flex-col gap-1">
            {response ? (
              <>
                <div className="flex items-center gap-1">
                  <Button size="xs" variant={raw ? "ghost" : "secondary"} onClick={() => setRaw(false)}>
                    Pretty
                  </Button>
                  <Button size="xs" variant={raw ? "secondary" : "ghost"} onClick={() => setRaw(true)}>
                    Raw
                  </Button>
                </div>
                <div className="relative min-h-40 flex-1 overflow-hidden rounded-md border border-border-subtle bg-surface-well">
                  <CodeEditor value={shown} onChange={() => undefined} readOnly extensions={extension} wrap="auto" />
                </div>
              </>
            ) : (
              <p className="pt-4 text-center text-caption-11 text-text-tertiary">
                {sending ? "Waiting for the answer…" : "Send the request to see the response here."}
              </p>
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
