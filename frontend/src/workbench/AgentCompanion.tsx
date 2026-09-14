import { useState } from "react";
import {
  agentHubCancel,
  agentHubGetStatus,
  agentHubInvoke,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  selectedPermissionOutcome,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { AgentRail } from "@/modules/agenthub/surfaces";
import { useAgentTranscript } from "@/modules/agenthub/transcript";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";

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
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const setPrompt = useAgentSessionsStore((s) => s.setPrompt);
  const pending = useAgentSessionsStore((s) => s.pending);
  const dequeuePending = useAgentSessionsStore((s) => s.dequeuePending);

  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [permBusy, setPermBusy] = useState(false);

  const session = sessions.find((s) => s.id === activeSessionId) ?? sessions[0] ?? null;
  const prompt = useAgentSessionsStore((s) => s.promptBySession[session?.id ?? ""] ?? "");

  // Live transcript stream for the active session in rail
  const transcriptModel = useAgentTranscript(session?.id ?? "");

  async function resolvePermission(optionId: string | null) {
    if (!pending) return;
    setPermBusy(true);
    setError("");
    try {
      await agentHubResolvePermission(
        pending.id,
        optionId === null ? cancelledPermissionOutcome() : selectedPermissionOutcome(optionId),
      );
      dequeuePending(pending.id);
      upsertSession(await agentHubGetStatus(pending.sessionId));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setPermBusy(false);
    }
  }

  async function handleSend() {
    if (!session || !prompt.trim() || busy) return;
    const text = prompt.trim();
    setPrompt(session.id, "");
    setBusy(true);
    setError("");
    try {
      upsertSession({ ...session, status: "running" });
      await agentHubInvoke({
        sessionId: session.id,
        prompt: text,
      });
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      setError(errorMessage(e));
      try {
        upsertSession(await agentHubGetStatus(session.id));
      } catch {}
    } finally {
      setBusy(false);
    }
  }

  async function handleCancel() {
    if (!session) return;
    try {
      await agentHubCancel(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  return (
    <AgentRail
      sessions={sessions}
      activeSession={session}
      onSelectSession={(id) => setActiveSessionId(id)}
      items={transcriptModel.items}
      usage={transcriptModel.usage}
      commands={transcriptModel.commands}
      promptValue={prompt}
      onPromptChange={(val) => session && setPrompt(session.id, val)}
      onSend={() => void handleSend()}
      onCancel={() => void handleCancel()}
      streaming={busy || session?.status === "running"}
      pendingPermission={pending}
      onResolvePermission={resolvePermission}
      permissionBusy={permBusy}
      errorMessage={error}
    />
  );
}
