import { useState } from "react";
import { agentHubGetStatus, agentHubResolvePermission, cancelledPermissionOutcome,
  parsePermissionOptions, permissionToolTitle, selectedPermissionOutcome } from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { Button } from "@/components/ui/button";
import { SessionTab } from "@/modules/agenthub/SessionTab";
import { EMPTY_TRANSCRIPT, useAgentSessionsStore } from "@/stores/agentSessions";

/**
 * The running conversation, in the shell's companion slot.
 *
 * Everything it renders comes from the store, so it is a view onto the same
 * conversation the Agent Hub module shows — not a second one. Switching modules
 * does not disturb it, because the shell owns this slot and the stream that
 * feeds it (see useAgentHubStream).
 *
 * Session creation stays in the Agent Hub module: that is a setup step with
 * vendor and working directory to choose, not something to do mid-conversation.
 */
export function AgentCompanion() {
  const sessions = useAgentSessionsStore((s) => s.sessions);
  const activeSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const transcripts = useAgentSessionsStore((s) => s.transcripts);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const setPrompt = useAgentSessionsStore((s) => s.setPrompt);
  const updateLines = useAgentSessionsStore((s) => s.updateLines);
  const pending = useAgentSessionsStore((s) => s.pending);
  const setPending = useAgentSessionsStore((s) => s.setPending);

  const [error, setError] = useState("");
  const [permBusy, setPermBusy] = useState(false);

  const session = sessions.find((s) => s.id === activeSessionId) ?? sessions[0];
  const transcript = session ? (transcripts[session.id] ?? EMPTY_TRANSCRIPT) : EMPTY_TRANSCRIPT;

  async function resolve(optionId: string | null) {
    if (!pending) return;
    setPermBusy(true);
    setError("");
    try {
      await agentHubResolvePermission(
        pending.id,
        optionId === null ? cancelledPermissionOutcome() : selectedPermissionOutcome(optionId),
      );
      setPending(null);
      upsertSession(await agentHubGetStatus(pending.sessionId));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPermBusy(false);
    }
  }

  if (!session) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-1 p-6 text-center">
        <p className="text-sm text-muted-foreground">No agent session yet.</p>
        <p className="text-xs text-text-tertiary">
          Open Agent Hub to start one — it needs a vendor and a working directory.
        </p>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 px-3 py-2.5 pr-12">
        <span className="truncate text-sm font-semibold text-text-primary">Agent</span>
        <span className="truncate font-mono text-[10px] text-text-tertiary">
          {session.agentType} · {session.status}
        </span>
      </div>

      {/* A permission request is the case where you press send and nothing
          happens, so it is stated here rather than only in the Agent Hub. */}
      {pending && pending.sessionId === session.id ? (
        <div className="mx-3 mb-2 shrink-0 rounded-lg border border-border-subtle bg-surface-tinted p-2.5">
          <p className="mb-2 text-xs text-text-primary">
            Waiting for permission: {permissionToolTitle(pending.params)}
          </p>
          <div className="flex flex-wrap gap-1.5">
            {parsePermissionOptions(pending.params).map((opt) => (
              <Button
                key={opt.optionId}
                size="sm"
                className="h-6 px-2 text-xs"
                disabled={permBusy}
                onClick={() => void resolve(opt.optionId)}
              >
                {opt.name}
              </Button>
            ))}
            <Button
              size="sm"
              variant="ghost"
              className="h-6 px-2 text-xs"
              disabled={permBusy}
              onClick={() => void resolve(null)}
            >
              Cancel
            </Button>
          </div>
        </div>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <SessionTab
          session={session}
          lines={transcript.lines}
          prompt={transcript.prompt}
          busy={false}
          onPromptChange={(prompt) => setPrompt(session.id, prompt)}
          onLinesChange={(updater) => updateLines(session.id, updater)}
          onSessionChange={upsertSession}
          onError={setError}
        />
      </div>

      {error ? <p className="shrink-0 px-3 pb-2 text-xs text-destructive">{error}</p> : null}
    </div>
  );
}
