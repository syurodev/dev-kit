import {
  agentHubCancel,
  agentHubGetStatus,
  agentHubInvoke,
  extractUpdateText,
  type AgentSession,
  type SessionStatus,
} from "../../bridge/agenthub";
import { errorMessage } from "../../bridge/errors";

export type TranscriptLine = {
  id: string;
  role: "user" | "agent" | "system";
  text: string;
};

type Props = {
  session: AgentSession;
  lines: TranscriptLine[];
  prompt: string;
  busy: boolean;
  onPromptChange: (value: string) => void;
  onLinesChange: (updater: (prev: TranscriptLine[]) => TranscriptLine[]) => void;
  onSessionChange: (session: AgentSession) => void;
  onError: (message: string) => void;
};

function statusLabel(status: SessionStatus): string {
  switch (status) {
    case "idle":
      return "idle";
    case "running":
      return "running";
    case "awaiting_permission":
      return "awaiting permission";
    case "error":
      return "error";
    case "closed":
      return "closed";
    default:
      return status || "unknown";
  }
}

export function SessionTab({
  session,
  lines,
  prompt,
  busy,
  onPromptChange,
  onLinesChange,
  onSessionChange,
  onError,
}: Props) {
  async function refreshStatus() {
    try {
      onSessionChange(await agentHubGetStatus(session.id));
    } catch (e) {
      onError(errorMessage(e));
    }
  }

  async function onSend() {
    const text = prompt.trim();
    if (!text || busy) {
      return;
    }
    onError("");
    const userLine: TranscriptLine = {
      id: `user-${Date.now()}`,
      role: "user",
      text,
    };
    onLinesChange((prev) => [...prev, userLine]);
    onPromptChange("");
    try {
      onSessionChange({ ...session, status: "running" });
      const result = await agentHubInvoke({
        sessionId: session.id,
        prompt: text,
      });
      const summary = result.summary.trim();
      if (summary) {
        onLinesChange((prev) => {
          const last = prev[prev.length - 1];
          if (
            last?.role === "agent" &&
            (last.text.includes(summary) || summary.includes(last.text))
          ) {
            return prev;
          }
          return [
            ...prev,
            {
              id: `agent-${Date.now()}`,
              role: "agent",
              text: summary,
            },
          ];
        });
      }
      await refreshStatus();
    } catch (e) {
      onError(errorMessage(e));
      await refreshStatus();
    }
  }

  async function onCancel() {
    onError("");
    try {
      await agentHubCancel(session.id);
      await refreshStatus();
    } catch (e) {
      onError(errorMessage(e));
    }
  }

  return (
    <div className="agenthub-tab">
      <div className="agenthub-tab-meta">
        <span className={`agenthub-status is-${session.status || "unknown"}`}>
          {statusLabel(session.status)}
        </span>
        <span className="agenthub-meta-path" title={session.projectPath}>
          {session.projectPath || "—"}
        </span>
        <span className="agenthub-meta-vendor">{session.agentType}</span>
      </div>

      <div className="agenthub-transcript" aria-label="agenthub-transcript">
        {lines.length === 0 ? (
          <p className="agenthub-empty">No messages yet. Send a prompt to start.</p>
        ) : (
          lines.map((line) => (
            <div
              key={line.id}
              className={`agenthub-line is-${line.role}`}
              data-role={line.role}
            >
              <span className="agenthub-line-role">{line.role}</span>
              <pre>{line.text}</pre>
            </div>
          ))
        )}
      </div>

      <div className="agenthub-composer">
        <textarea
          aria-label="agenthub-prompt"
          placeholder="Prompt…"
          value={prompt}
          disabled={busy || session.status === "closed"}
          onChange={(e) => onPromptChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void onSend();
            }
          }}
        />
        <div className="agenthub-composer-actions">
          <button
            type="button"
            onClick={() => void onSend()}
            disabled={
              busy ||
              !prompt.trim() ||
              session.status === "running" ||
              session.status === "awaiting_permission" ||
              session.status === "closed"
            }
          >
            Send
          </button>
          <button
            type="button"
            onClick={() => void onCancel()}
            disabled={
              session.status !== "running" &&
              session.status !== "awaiting_permission"
            }
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

/** Append streaming chunk text for a session update payload. */
export function appendUpdateLine(
  lines: TranscriptLine[],
  update: unknown,
): TranscriptLine[] {
  const text = extractUpdateText(update).trim();
  if (!text) {
    return lines;
  }
  const last = lines[lines.length - 1];
  if (last?.role === "agent") {
    return [
      ...lines.slice(0, -1),
      { ...last, text: last.text === text ? last.text : `${last.text}${text}` },
    ];
  }
  return [
    ...lines,
    {
      id: `agent-${Date.now()}-${lines.length}`,
      role: "agent",
      text,
    },
  ];
}
