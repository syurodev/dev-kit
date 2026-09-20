import { useCallback, useEffect, useState } from "react";
import { Folder, Send, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  curlFor,
  listCollections,
  listEnvironments,
  listRequests,
  missingVars,
  saveRequest,
  sendRequest,
  type ApiCollection,
  type ApiEnvironment,
  type ApiRequest,
  type ApiResponse,
} from "@/bridge/apiclient";
import { ApiAuthPicker } from "./ApiAuthPicker";
import { CollectionsDialog } from "./CollectionsDialog";
import { RequestHistory } from "./RequestHistory";
import { byFolderThenName, folderPath, foldersByPath, requestLabel } from "./collections";

/**
 * Compose and send. Auth is not a field here on purpose: the request carries
 * an account id, the value is resolved in Go at send time, and this view never
 * holds a credential — which is also why Copy as cURL is safe to paste.
 */
export function ApiRequestView() {
  const [requests, setRequests] = useState<ApiRequest[] | null>(null);
  // null until loaded: the environment picker showing "No environment"
  // alone before the list arrives reads as "you have none".
  const [environments, setEnvironments] = useState<ApiEnvironment[] | null>(null);
  // null until loaded, like the environments: an empty folder list and one
  // that has not arrived look the same in a picker and mean different things.
  const [collections, setCollections] = useState<ApiCollection[] | null>(null);
  const [foldersOpen, setFoldersOpen] = useState(false);
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
        setRequestID((current) => current || next[0]?.id || "");
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
  }, []);

  useEffect(reload, [reload]);

  if (error && !requests) {
    return <EmptyState icon={Send} title="API Client is unavailable" body={error} />;
  }
  if (!requests || !environments || !collections) {
    return null;
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
            <pre className="whitespace-pre-wrap pt-2 font-mono text-xs">{response.body}</pre>
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
