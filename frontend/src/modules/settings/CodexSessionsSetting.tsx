import { useEffect, useState } from "react";
import { Bot, ChevronDown, ChevronRight, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { errorMessage } from "../../bridge/errors";
import {
  clearAllCodexSessions,
  clearCodexSession,
  codexSessionEvents,
  listCodexSessions,
  type CodexSession,
  type CodexSessionEvent,
} from "../../bridge/codexsessions";

/**
 * What DevKit has observed of Codex CLI sessions running outside it.
 *
 * DevKit does not spawn Codex and is not on its stdio — this is Layer 1 of
 * Codex support (docs/dev-kit/agent-hub.mdx): Codex's own lifecycle hooks call
 * back into DevKit's MCP server, which is what `task mcp:install` writes into
 * `~/.codex/config.toml` for a Codex agent. There is no way to send a prompt
 * from here, and no permission gate — see codexhook's package doc for why
 * DevKit does not answer PermissionRequest yet: watching comes first,
 * deciding is a separate feature with a separate switch.
 */
export function CodexSessionsSetting() {
  const [sessions, setSessions] = useState<CodexSession[] | null>(null);
  const [expanded, setExpanded] = useState("");
  const [events, setEvents] = useState<CodexSessionEvent[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const reload = () => {
    listCodexSessions()
      .then((next) => {
        setSessions(next);
        setError("");
      })
      .catch((e: unknown) => setError(errorMessage(e)));
  };

  useEffect(reload, []);

  useEffect(() => {
    if (!expanded) {
      setEvents(null);
      return;
    }
    codexSessionEvents(expanded)
      .then(setEvents)
      .catch((e: unknown) => setError(errorMessage(e)));
  }, [expanded]);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <p className="text-body-12 text-text-tertiary">
          Sessions run in your own terminal; DevKit only sees what their hooks
          report.
        </p>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Reload"
            onClick={reload}
          >
            <RefreshCw className="size-4" aria-hidden />
          </Button>
          {sessions && sessions.length > 0 ? (
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={() => {
                setBusy(true);
                clearAllCodexSessions()
                  .then(() => {
                    setExpanded("");
                    reload();
                  })
                  .catch((e: unknown) => setError(errorMessage(e)))
                  .finally(() => setBusy(false));
              }}
            >
              Clear all
            </Button>
          ) : null}
        </div>
      </div>

      {error ? <p className="text-body-12 text-status-danger">{error}</p> : null}

      {sessions === null ? null : sessions.length === 0 ? (
        <p className="rounded-control border border-border-subtle bg-surface-raised px-3.5 py-2.5 text-body-13 text-text-tertiary">
          No Codex sessions observed yet. Install Codex support from the MCP
          panel, then run <code className="text-code-12">codex</code> in a
          project — its first prompt shows up here.
        </p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {sessions.map((session) => (
            <li
              key={session.id}
              className="rounded-control border border-border-subtle bg-surface-raised"
            >
              <button
                type="button"
                className="flex w-full items-center gap-2.5 px-3.5 py-2.5 text-left"
                onClick={() => setExpanded((current) => (current === session.id ? "" : session.id))}
                aria-expanded={expanded === session.id}
              >
                {expanded === session.id ? (
                  <ChevronDown className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
                ) : (
                  <ChevronRight className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
                )}
                <Bot className="size-4 shrink-0 text-text-secondary" aria-hidden />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-body-13 text-text-primary">
                    {session.cwd || session.id}
                  </div>
                  <div className="truncate text-code-11 text-text-tertiary">
                    {session.model || "unknown model"} · {session.eventCount} event
                    {session.eventCount === 1 ? "" : "s"} · last {session.lastEvent}
                  </div>
                </div>
                <span
                  role="button"
                  tabIndex={0}
                  aria-label={`Clear session ${session.id}`}
                  className="rounded p-1 text-text-tertiary hover:bg-state-soft hover:text-status-danger"
                  onClick={(event) => {
                    event.stopPropagation();
                    clearCodexSession(session.id)
                      .then(() => {
                        if (expanded === session.id) setExpanded("");
                        reload();
                      })
                      .catch((e: unknown) => setError(errorMessage(e)));
                  }}
                  onKeyDown={(event) => {
                    if (event.key !== "Enter" && event.key !== " ") return;
                    event.preventDefault();
                    event.stopPropagation();
                    clearCodexSession(session.id)
                      .then(() => {
                        if (expanded === session.id) setExpanded("");
                        reload();
                      })
                      .catch((e: unknown) => setError(errorMessage(e)));
                  }}
                >
                  <Trash2 className="size-3.5" aria-hidden />
                </span>
              </button>

              {expanded === session.id ? (
                <div className="border-t border-border-subtle px-3.5 py-2.5">
                  {events === null ? (
                    <p className="text-code-11 text-text-tertiary">Loading…</p>
                  ) : events.length === 0 ? (
                    <p className="text-code-11 text-text-tertiary">Nothing recorded.</p>
                  ) : (
                    <ol className="flex flex-col gap-1.5">
                      {events.map((event) => (
                        <li key={event.id} className="text-code-11">
                          <span className="text-text-tertiary">{clockOf(event.occurredAt)}</span>{" "}
                          <span className="text-text-secondary">{event.summary}</span>
                        </li>
                      ))}
                    </ol>
                  )}
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Local time, to the minute — a transcript answers "when", not "exactly when". */
function clockOf(iso: string): string {
  const at = new Date(iso);
  return Number.isNaN(at.getTime())
    ? ""
    : at.toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
}
