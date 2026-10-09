import { useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent } from "react";
import { toast } from "sonner";
import { Send, Wand2 } from "lucide-react";
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
  type BodyMode,
  type RequestInput,
  type ResolvedVar,
} from "@/bridge/apiclient";
import { copyText } from "@/bridge/clipboard";
import { errorMessage, isCancelError } from "@/bridge/errors";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsCount, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ListSkeleton } from "@/components/ui/skeleton";
import { ResponsePane } from "./ResponsePane";
import { RequestToolbar } from "./RequestToolbar";
import { tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { activeEnvironment, tagAsks, useApiClientStore, type EditorState } from "./apiclientStore";
import { EnvTagChip } from "./ApiClientSidebar";
import { AuthPanel } from "./AuthPanel";
import { folderPath, foldersByPath } from "./collections";
import { activeApiClientTab, closeTabByParams, MODULE, openRequest, openVariables, renameApiDocument, REQUEST_VIEW } from "./documents";
import { KeyValueTable } from "./KeyValueTable";
import {
  activeRows,
  emptyRequest,
  parseUrlBar,
  prettyBody,
  referencedVars,
  sameRequest,
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
  // An endpoint id means something only in the index it was listed in: the same
  // id in two workspace folders is two endpoints.
  if (typeof params?.endpointId === "string" && params.endpointId) {
    return `endpoint:${typeof params.project === "string" ? params.project : ""}:${params.endpointId}`;
  }
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
      draftFromEndpoint(params.endpointId, typeof params.project === "string" ? params.project : "")
        .then((draft) => setEditor(key, freshEditor(forEditor({ ...draft, id: "" }), null, draft.bodyInferred ? "" : draft.note)))
        .catch((e) => setLoadError(errorMessage(e)));
    } else {
      setEditor(key, freshEditor(emptyRequest(), null));
    }
  }, [key, params?.requestId, params?.newId, params?.endpointId, params?.project]);

  if (loadError) {
    return <EmptyState icon={Send} title="This request could not be opened" body={loadError} />;
  }
  if (!editor) return <ListSkeleton rows={5} />;
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
    if (sending) return;
    try {
      const saved = forEditor(await saveRequest(request));
      if (request.id) {
        store.patchEditor(key, { request: saved, baseline: saved });
        renameApiDocument(REQUEST_VIEW, { requestId: saved.id }, saved.name);
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
    (s) => s.activeModuleId === MODULE && activeApiClientTab(s)?.id === tabKey(MODULE, REQUEST_VIEW, params),
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
      <RequestToolbar
        name={request.name} onNameChange={(name) => update({ name })} dirty={dirty}
        folders={folders.map((f) => ({ id: f.id, label: folderPath(collections ?? [], f.id) }))}
        folderId={request.collectionId} onFolderChange={(collectionId) => update({ collectionId })}
        environments={(environments ?? []).map((e) => ({ id: e.id, label: e.name }))}
        environmentId={environment?.id ?? ""} onEnvironmentChange={store.setActiveEnvironment}
        environmentTag={environment ? <EnvTagChip tag={environment.env} /> : undefined} gated={gated}
        onManageEnvironment={() => openVariables("environment", environment?.id ?? "new", environment?.name ?? "New environment")}
        url={urlDraft ?? urlBarText(request.url, request.query)}
        onUrlFocus={() => setUrlDraft(urlBarText(request.url, request.query))} onUrlBlur={() => setUrlDraft(null)}
        onUrlChange={(value) => { setUrlDraft(value); setUrlText(value); }} onUrlPaste={onUrlPaste}
        method={request.method} onMethodChange={(method) => update({ method })}
        sending={sending} onSend={send} onCancel={cancel} onSave={() => void save()}
        onCurl={() => {
          curlFor(request, environment?.id ?? "").then((text) => {
            setCurl(text);
            return copyText(text).then(() => toast.success("cURL copied"));
          }).catch((e) => toast.error("Could not build the cURL command", { description: errorMessage(e) }));
        }}
      />

      {missing.length > 0 || !environment || editor.note || request.originRef ? (
        <div className="flex flex-col gap-1 border-b border-border-subtle bg-surface-well/50 px-4 py-2 text-caption-11">
          {!environment ? (
            <p className="text-status-warning">
              Choose an environment before sending. Use the settings button above to create one.
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
          <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-3 px-4 pt-2">
            <TabsList variant="line" className="max-w-full w-fit overflow-x-auto">
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
            <div className="min-h-0 flex-1 overflow-auto overscroll-contain pb-3">
              <TabsContent value="params" className="flex flex-col gap-3">
                <p className="text-caption-11 text-text-tertiary">Query parameters are appended to the URL. Uncheck a row to keep it out of this request.</p>
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
