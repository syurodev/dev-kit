import { useCallback, useEffect, useState } from "react";
import { Send, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Textarea } from "@/components/ui/textarea";
import {
  curlFor,
  listEnvironments,
  listRequests,
  missingVars,
  sendRequest,
  type ApiEnvironment,
  type ApiRequest,
  type ApiResponse,
} from "@/bridge/apiclient";

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
  }, []);

  useEffect(reload, [reload]);

  if (error && !requests) {
    return <EmptyState icon={Send} title="API Client is unavailable" body={error} />;
  }
  if (!requests || !environments) {
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
          {requests.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.method} {candidate.name}
            </option>
          ))}
        </select>
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
        {request.auth.accountId ? (
          <p className="pt-1 text-xs text-fg-muted">
            Authenticates with a saved account ({request.auth.kind})
          </p>
        ) : null}
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
      </div>
    </div>
  );
}
