// frontend/src/modules/agenthub/AgentHubView.tsx
import { useEffect, useMemo, useState } from "react";
import {
  agentHubCreateSession,
  agentHubGetStatus,
  agentHubListVendorTypes,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  isSandboxUnavailableError,
  onAgentHubPermission,
  onAgentHubSessionUpdate,
  parsePermissionOptions,
  permissionToolTitle,
  sandboxUnavailableMessage,
  selectedPermissionOutcome,
  type AgentVendor,
  type PendingPermission,
} from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";
import { appendUpdateLine, SessionTab, type TranscriptLine } from "./SessionTab";
import { useAgentSessionsStore } from "../../stores/agentSessions";

type TranscriptState = {
  lines: TranscriptLine[];
  prompt: string;
};

const DEFAULT_VENDORS: AgentVendor[] = ["mock", "claude"];
const EMPTY_TRANSCRIPT: TranscriptState = { lines: [], prompt: "" };

export function AgentHubView() {
  const sessions = useAgentSessionsStore((state) => state.sessions);
  const activeSessionId = useAgentSessionsStore((state) => state.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((state) => state.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);

  const [vendors, setVendors] = useState<string[]>(DEFAULT_VENDORS);
  const [vendor, setVendor] = useState<string>("mock");
  const [cwd, setCwd] = useState(".");
  const [transcripts, setTranscripts] = useState<Record<string, TranscriptState>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [sandboxHint, setSandboxHint] = useState("");
  const [pending, setPending] = useState<PendingPermission | null>(null);
  const [permBusy, setPermBusy] = useState(false);

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? sessions[0],
    [sessions, activeSessionId],
  );
  const activeTranscript = activeSession
    ? transcripts[activeSession.id] ?? EMPTY_TRANSCRIPT
    : EMPTY_TRANSCRIPT;

  useEffect(() => {
    if (!activeSessionId && sessions.length > 0) {
      setActiveSessionId(sessions[0].id);
    }
  }, [sessions, activeSessionId, setActiveSessionId]);

  useEffect(() => {
    let mounted = true;
    agentHubListVendorTypes()
      .then((list) => {
        if (!mounted || list.length === 0) {
          return;
        }
        setVendors(list);
        if (!list.includes(vendor)) {
          setVendor(list[0]);
        }
      })
      .catch(() => {
        // Keep mock|claude defaults when listing fails (e.g. hub unavailable).
      });
    return () => {
      mounted = false;
    };
  }, []);

  useEffect(() => {
    const offUpdate = onAgentHubSessionUpdate((event) => {
      setTranscripts((prev) => {
        const current = prev[event.sessionId] ?? EMPTY_TRANSCRIPT;
        return {
          ...prev,
          [event.sessionId]: {
            ...current,
            lines: appendUpdateLine(current.lines, event.update),
          },
        };
      });
    });
    const offPermission = onAgentHubPermission((next) => {
      setPending(next);
    });
    return () => {
      offUpdate();
      offPermission();
    };
  }, []);

  async function onNewTab() {
    setBusy(true);
    setError("");
    setSandboxHint("");
    try {
      const session = await agentHubCreateSession({
        agentType: vendor,
        cwd: cwd.trim() || ".",
      });
      upsertSession(session);
      setActiveSessionId(session.id);
    } catch (e) {
      setError(errorMessage(e));
      if (vendor === "claude" && isSandboxUnavailableError(e)) {
        setSandboxHint(sandboxUnavailableMessage());
      }
    } finally {
      setBusy(false);
    }
  }

  async function resolvePermission(optionId: string | null) {
    if (!pending) {
      return;
    }
    setPermBusy(true);
    setError("");
    try {
      const outcome =
        optionId === null
          ? cancelledPermissionOutcome()
          : selectedPermissionOutcome(optionId);
      await agentHubResolvePermission(pending.id, outcome);
      setPending(null);
      try {
        upsertSession(await agentHubGetStatus(pending.sessionId));
      } catch (e) {
        setError(errorMessage(e));
      }
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPermBusy(false);
    }
  }

  const permissionOptions = pending
    ? parsePermissionOptions(pending.params)
    : [];

  return (
    <section className="agenthub-view">
      <h2>Agent Hub</h2>

      <div className="agenthub-header">
        <label>
          Vendor
          <select
            aria-label="agenthub-vendor"
            value={vendor}
            disabled={busy}
            onChange={(e) => setVendor(e.target.value)}
          >
            {vendors.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label className="agenthub-cwd">
          Working directory
          <input
            aria-label="agenthub-cwd"
            value={cwd}
            disabled={busy}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="."
          />
        </label>
        <button type="button" onClick={() => void onNewTab()} disabled={busy}>
          New tab
        </button>
      </div>

      {activeSession ? (
        <SessionTab
          session={activeSession}
          lines={activeTranscript.lines}
          prompt={activeTranscript.prompt}
          busy={busy}
          onPromptChange={(prompt) =>
            setTranscripts((prev) => ({
              ...prev,
              [activeSession.id]: { ...(prev[activeSession.id] ?? EMPTY_TRANSCRIPT), prompt },
            }))
          }
          onLinesChange={(updater) =>
            setTranscripts((prev) => ({
              ...prev,
              [activeSession.id]: {
                ...(prev[activeSession.id] ?? EMPTY_TRANSCRIPT),
                lines: updater(prev[activeSession.id]?.lines ?? []),
              },
            }))
          }
          onSessionChange={(session) => upsertSession(session)}
          onError={setError}
        />
      ) : (
        <p className="agenthub-empty">No sessions. Create a tab to begin.</p>
      )}

      {sandboxHint ? (
        <div className="agenthub-sandbox-error" role="alert">
          <p>{sandboxHint}</p>
          <button
            type="button"
            onClick={() => {
              setVendor("mock");
              setSandboxHint("");
              setError("");
            }}
          >
            Switch to mock vendor
          </button>
        </div>
      ) : null}

      {error ? <p role="alert">{error}</p> : null}

      {pending ? (
        <div className="agenthub-permission-backdrop" role="presentation">
          <div
            className="agenthub-permission-modal"
            role="dialog"
            aria-modal="true"
            aria-label="agenthub-permission"
          >
            <h3>{permissionToolTitle(pending.params)}</h3>
            <p>
              Session <code>{pending.sessionId.slice(0, 8)}</code> needs a
              permission decision.
            </p>
            <pre className="agenthub-permission-params">
              {safeJson(pending.params)}
            </pre>
            <div className="agenthub-permission-actions">
              <button
                type="button"
                disabled={permBusy}
                onClick={() => void resolvePermission(null)}
              >
                Deny
              </button>
              {permissionOptions.map((opt) => (
                <button
                  key={opt.optionId}
                  type="button"
                  disabled={permBusy}
                  onClick={() => void resolvePermission(opt.optionId)}
                >
                  {opt.name}
                </button>
              ))}
              {permissionOptions.length === 0 ? (
                <button
                  type="button"
                  disabled={permBusy}
                  onClick={() => void resolvePermission("allow")}
                >
                  Allow
                </button>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function safeJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
