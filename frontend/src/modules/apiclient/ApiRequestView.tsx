import { useCallback, useEffect, useState } from "react";
import { Folder, Save, Send, Terminal } from "lucide-react";
import { CodeBlock } from "@/components/code/CodeBlock";
import { detectLanguage, languageForContentType } from "@/components/code/languages";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  curlFor,
  draftFromEndpoint,
  listCollections,
  listEnvironments,
  listRequests,
  missingVars,
  saveRequest,
  sendRequest,
  type ApiCollection,
  type ApiDraft,
  type ApiEnvironment,
  type ApiRequest,
  type ApiResponse,
} from "@/bridge/apiclient";
import { ApiAuthPicker } from "./ApiAuthPicker";
import { CollectionsDialog } from "./CollectionsDialog";
import { RequestHistory } from "./RequestHistory";
import { byFolderThenName, folderPath, foldersByPath, requestLabel } from "./collections";


/**
 * A response body's language: what the server said it is, then what it
 * looks like — plenty of APIs send JSON as text/plain, or no type at all.
 */
function bodyLanguage(headers: Record<string, string> | null, body: string): string | undefined {
  const contentType = Object.entries(headers ?? {}).find(([name]) => name.toLowerCase() === "content-type")?.[1];
  return languageForContentType(contentType) ?? detectLanguage(body);
}

/**
 * Compose and send. Auth is not a field here on purpose: the request carries
 * an account id, the value is resolved in Go at send time, and this view never
 * holds a credential — which is also why Copy as cURL is safe to paste.
 */
export function ApiRequestView({ params }: { params?: Record<string, unknown> } = {}) {
  // An endpoint id rather than a draft: a tab reopened after a restart asks
  // for a draft of the CURRENT index, not one built against an index that has
  // since changed.
  const endpointID = typeof params?.endpointId === "string" ? params.endpointId : "";
  // A saved request this tab was asked to open on — `api.reveal`
  // (docs/dev-kit/cross-module.mdx), a ToolResultView card's "Try in API
  // Client" for `api.send`. Only a starting point: the picker below still
  // owns which request is showing once the tab is open.
  const requestIDParam = typeof params?.requestId === "string" ? params.requestId : "";
  const [requests, setRequests] = useState<ApiRequest[] | null>(null);
  // null until loaded: the environment picker showing "No environment"
  // alone before the list arrives reads as "you have none".
  const [environments, setEnvironments] = useState<ApiEnvironment[] | null>(null);
  // null until loaded, like the environments: an empty folder list and one
  // that has not arrived look the same in a picker and mean different things.
  const [collections, setCollections] = useState<ApiCollection[] | null>(null);
  const [foldersOpen, setFoldersOpen] = useState(false);
  // The unsaved draft this tab was opened on. A draft becomes a request only
  // when somebody saves it, so nothing here writes until they do.
  const [draft, setDraft] = useState<ApiDraft | null>(null);
  const [requestID, setRequestID] = useState("");
  const [environmentID, setEnvironmentID] = useState("");
  const [response, setResponse] = useState<ApiResponse | null>(null);
  const [curl, setCurl] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");

  const reload = useCallback(() => {
    listRequests()
      .then((next) => {
        setRequests(next);
        setRequestID((current) => current || requestIDParam || next[0]?.id || "");
      })
      .catch((e: Error) => setError(e.message));
    listEnvironments()
      .then((next) => {
        setEnvironments(next);
        setEnvironmentID((current) => current || next[0]?.id || "");
      })
      .catch(() => setEnvironments([]));
    listCollections()
      .then(setCollections)
      .catch(() => setCollections([]));
  }, [requestIDParam]);

  useEffect(reload, [reload]);

  useEffect(() => {
    if (!endpointID) return;
    draftFromEndpoint(endpointID)
      .then(setDraft)
      .catch((e: Error) => setError(e.message));
  }, [endpointID]);

  if (error && !requests) {
    return <EmptyState icon={Send} title="API Client is unavailable" body={error} />;
  }
  if (!requests || !environments || !collections) {
    return null;
  }
  if (draft) {
    return (
      <DraftView
        draft={draft}
        environments={environments}
        onSaved={(saved) => {
          setDraft(null);
          setRequestID(saved.id);
          reload();
        }}
      />
    );
  }
  if (requests.length === 0) {
    return (
      <EmptyState
        icon={Send}
        title="No requests yet"
        body="Try an endpoint from Code Graph, or save one here."
      />
    );
  }

  const request = requests.find((candidate) => candidate.id === requestID) ?? requests[0];
  const environment = environments.find((candidate) => candidate.id === environmentID) ?? null;
  const vars = environment?.vars ?? {};
  const missing = missingVars(request.url, vars);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <select
          className="bg-transparent text-sm"
          value={request.id}
          aria-label="Request"
          onChange={(event) => {
            setRequestID(event.target.value);
            setResponse(null);
            setCurl("");
          }}
        >
          {byFolderThenName(collections, requests).map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.method} {requestLabel(collections, candidate)}
            </option>
          ))}
        </select>
        {/* Filing the open request, rather than a tree in a sidebar: the
            folder is one fact about a request, and this is where a request is
            already being looked at. */}
        <select
          className="bg-transparent text-sm"
          value={request.collectionId ?? ""}
          aria-label="Folder"
          onChange={(event) => {
            saveRequest({ ...request, collectionId: event.target.value })
              .then(reload)
              .catch((e: Error) => setError(e.message));
          }}
        >
          <option value="">No folder</option>
          {foldersByPath(collections).map((folder) => (
            <option key={folder.id} value={folder.id}>
              {folderPath(collections, folder.id)}
            </option>
          ))}
        </select>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Manage folders"
          onClick={() => setFoldersOpen(true)}
        >
          <Folder className="size-4" aria-hidden />
        </Button>
        <div className="flex-1" />
        <select
          className="bg-transparent text-sm"
          value={environmentID}
          aria-label="Environment"
          onChange={(event) => setEnvironmentID(event.target.value)}
        >
          <option value="">No environment</option>
          {environments.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.name} ({candidate.env})
            </option>
          ))}
        </select>
      </header>

      <div className="border-b border-border-subtle px-3 py-2">
        <code className="text-xs">{request.url}</code>
        {missing.length > 0 ? (
          // Unfilled variables stay visible in the URL rather than collapsing
          // to nothing, so the reason a request cannot go is on screen.
          <p className="pt-1 text-xs text-amber-500">
            Not set in this environment: {missing.join(", ")}
          </p>
        ) : null}
        <ApiAuthPicker
          request={request}
          env={environment?.env ?? ""}
          vars={vars}
          onSaved={reload}
        />
      </div>

      <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <Button
          size="sm"
          disabled={sending || environmentID === ""}
          title={environmentID === "" ? "Pick an environment first" : undefined}
          onClick={() => {
            setSending(true);
            setError("");
            sendRequest(request.id, environmentID)
              .then(setResponse)
              .catch((e: Error) => setError(e.message))
              .finally(() => setSending(false));
          }}
        >
          <Send className="size-4" aria-hidden />
          {sending ? "Sending…" : "Send"}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            curlFor(request.id, vars)
              .then(setCurl)
              .catch((e: Error) => setError(e.message));
          }}
        >
          <Terminal className="size-4" aria-hidden />
          Copy as cURL
        </Button>
      </div>

      {error ? <p className="px-3 py-2 text-xs text-red-500">{error}</p> : null}

      <div className="min-h-0 flex-1 overflow-auto p-3">
        {curl ? (
          <Textarea readOnly value={curl} rows={6} aria-label="cURL command" className="font-mono text-xs" />
        ) : null}
        {response ? (
          <section className={curl ? "pt-3" : undefined}>
            <div className="text-xs text-fg-muted">
              {response.status} {response.statusText} · {response.durationMs} ms ·{" "}
              {response.bytes} bytes
              {response.truncated ? " · truncated" : ""}
            </div>
            <CodeBlock
              code={response.body}
              language={bodyLanguage(response.headers, response.body)}
              className="whitespace-pre-wrap pt-2 font-mono text-xs"
            />
          </section>
        ) : null}
        {/* Keyed on the request so switching requests remounts it: a list of
            sends left over from the previous one is worse than none. */}
        <RequestHistory key={request.id} requestID={request.id} />
      </div>
      <CollectionsDialog
        open={foldersOpen}
        onClose={() => setFoldersOpen(false)}
        onChanged={reload}
      />
    </div>
  );
}


/**
 * A draft from a Code Graph endpoint, before it is a request.
 *
 * It is read-only except for Save. FR-7 is explicit that a draft becomes a
 * request only when the person saves it, and offering Send on something that
 * exists nowhere would leave them with a response and no record of what
 * produced it.
 *
 * The note is shown whenever the body was not read from the code. Showing an
 * empty `{}` instead would say the DTO is empty, which is the one thing none
 * of the three reasons means.
 */
function DraftView({
  draft,
  environments,
  onSaved,
}: {
  draft: ApiDraft;
  environments: ApiEnvironment[];
  onSaved: (saved: ApiRequest) => void;
}) {
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const vars = environments[0]?.vars ?? {};

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
        <span className="text-sm font-medium">{draft.method}</span>
        <code className="min-w-0 flex-1 truncate text-xs">{draft.url}</code>
        <span className="shrink-0 text-xs text-fg-muted">draft · not saved</span>
        <Button
          size="sm"
          disabled={saving}
          onClick={() => {
            setSaving(true);
            setError("");
            saveRequest(draft)
              .then(onSaved)
              .catch((e: Error) => setError(e.message))
              .finally(() => setSaving(false));
          }}
        >
          <Save className="size-4" aria-hidden />
          Save request
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-auto p-3 text-xs">
        {error ? <p className="pb-2 text-red-500">{error}</p> : null}
        {missingVars(draft.url, vars).length > 0 ? (
          <p className="pb-2 text-amber-500">
            Not set in this environment: {missingVars(draft.url, vars).join(", ")}
          </p>
        ) : null}

        <DraftSection title="Query" pairs={draft.query} />
        <DraftSection title="Headers" pairs={draft.headers} />

        <section className="pt-3">
          <h3 className="pb-1 font-medium text-fg-muted">Body</h3>
          {draft.bodyInferred ? (
            <CodeBlock code={draft.body ?? ""} language={detectLanguage(draft.body ?? "")} className="whitespace-pre-wrap font-mono" />
          ) : (
            <p className="text-fg-muted">{draft.note}</p>
          )}
        </section>

        <p className="pt-4 text-fg-muted">
          Drafted from a Code Graph endpoint. Save it to send it — a draft is not a
          request until you keep it.
        </p>
      </div>
    </div>
  );
}

function DraftSection({ title, pairs }: { title: string; pairs?: Record<string, string> | null }) {
  const entries = Object.entries(pairs ?? {});
  if (entries.length === 0) return null;
  return (
    <section className="pt-3">
      <h3 className="pb-1 font-medium text-fg-muted">{title}</h3>
      <ul className="font-mono">
        {entries.map(([key, value]) => (
          <li key={key}>
            {key}: {value}
          </li>
        ))}
      </ul>
    </section>
  );
}
