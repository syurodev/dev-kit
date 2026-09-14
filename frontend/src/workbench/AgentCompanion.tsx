import { useEffect, useState } from "react";
import {
  agentHubCancel,
  agentHubGetStatus,
  agentHubInvoke,
  agentHubChooseFiles,
  agentHubListContextFiles,
  agentHubSetConfigOption,
  agentHubSetMode,
  agentHubResolvePermission,
  cancelledPermissionOutcome,
  selectedPermissionOutcome,
} from "@/bridge/agenthub";
import { errorMessage } from "@/bridge/errors";
import { AgentRail } from "@/modules/agenthub/surfaces";
import { updateTranscriptControls, useAgentTranscript } from "@/modules/agenthub/transcript";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { openCodeGraphProject, readCodeGraphFile } from "@/bridge/codegraph";
import { useCodeGraphStore } from "@/modules/codegraph/codegraphStore";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

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
export function AgentCompanion({ visible = true }: { visible?: boolean }) {
  const sessions = useAgentSessionList();
  const activeSessionId = useAgentSessionsStore((s) => s.activeSessionId);
  const setActiveSessionId = useAgentSessionsStore((s) => s.setActiveSessionId);
  const openTab = useWorkbenchTabsStore((s) => s.openTab);
  const upsertSession = useAgentSessionsStore((s) => s.upsertSession);
  const setPrompt = useAgentSessionsStore((s) => s.setPrompt);
  const pending = useAgentSessionsStore((s) => s.pending);
  const dequeuePending = useAgentSessionsStore((s) => s.dequeuePending);

  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [permBusy, setPermBusy] = useState(false);
  const [attachments, setAttachments] = useState<string[]>([]);
  const [contextFiles, setContextFiles] = useState<Awaited<ReturnType<typeof agentHubListContextFiles>>>([]);

  const session = sessions.find((s) => s.id === activeSessionId) ?? sessions[0] ?? null;
  const prompt = useAgentSessionsStore((s) => s.promptBySession[session?.id ?? ""] ?? "");

  // Live transcript stream for the active session in rail
  const transcriptModel = useAgentTranscript(session?.id ?? "", { enabled: visible });

  useEffect(() => {
    if (!visible || !session) return;
    let mounted = true;
    agentHubListContextFiles(session.id)
      .then((files) => mounted && setContextFiles(files))
      .catch(() => mounted && setContextFiles([]));
    return () => { mounted = false; };
  }, [session?.id, visible]);

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

  async function handleSend(inputText?: string) {
    if (!session || (!(inputText ?? prompt.trim()) && attachments.length === 0) || busy) return;
    const text = inputText ?? prompt.trim();
    setPrompt(session.id, "");
    const attached = attachments;
    setBusy(true);
    setError("");
    try {
      upsertSession({ ...session, status: "running" });
      await agentHubInvoke({
        sessionId: session.id,
        prompt: text,
        attachments: attached,
      });
      upsertSession(await agentHubGetStatus(session.id));
      setAttachments([]);
    } catch (e) {
      setError(errorMessage(e));
      try {
        upsertSession(await agentHubGetStatus(session.id));
      } catch {}
    } finally {
      setBusy(false);
    }
  }

  function selectCommand(name: string) {
    if (!session) return;
    const command = transcriptModel.commands.find((item) => item.name.replace(/^\//, "") === name.replace(/^\//, ""));
    const commandName = name.startsWith("/") ? name : `/${name}`;
    if (command?.input) {
      const current = useAgentSessionsStore.getState().promptBySession[session.id] ?? "";
      setPrompt(session.id, `${current}${current && !/\s$/.test(current) ? " " : ""}${commandName} `);
    } else {
      void handleSend(commandName);
    }
  }

  function selectContext(path: string) {
    setAttachments((current) => current.includes(path) ? current : [...current, path]);
  }

  async function openFile(path: string, line?: number) {
    if (!session) return;
    const root = session.projectPath.replace(/[\\/]+$/, "");
    const normalizedPath = path.split("\\").join("/");
    const normalizedRoot = root.split("\\").join("/");
    const relative = normalizedPath.startsWith(`${normalizedRoot}/`) ? normalizedPath.slice(normalizedRoot.length + 1) : normalizedPath;
    try {
      await openCodeGraphProject(root);
      const content = await readCodeGraphFile(relative);
      useCodeGraphStore.getState().set({ projectPath: root, activeTab: "files", selectedFile: relative, selectedLine: line ?? null, fileContent: content, fileDirty: false, fileError: "" });
      openTab({ moduleId: "codegraph", viewId: "codegraph.explorer", title: "Code Graph" });
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function setMode(nextMode: string) {
    if (!session) return;
    const previous = transcriptModel;
    updateTranscriptControls(session.id, (model) => ({ ...model, mode: nextMode }));
    try { await agentHubSetMode(session.id, nextMode); }
    catch (e) { updateTranscriptControls(session.id, () => previous); setError(errorMessage(e)); }
  }

  async function setConfigOption(optionId: string, value: string) {
    if (!session) return;
    const previous = transcriptModel;
    updateTranscriptControls(session.id, (model) => ({
      ...model,
      configOptions: model.configOptions.map((option) => option.id === optionId ? { ...option, currentValue: value } : option),
    }));
    try { await agentHubSetConfigOption(session.id, optionId, value); }
    catch (e) { updateTranscriptControls(session.id, () => previous); setError(errorMessage(e)); }
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
      contextFiles={contextFiles}
      onSelectCommand={selectCommand}
      onSelectContext={selectContext}
      attachedFiles={attachments}
      onRemoveAttachment={(path) => setAttachments((current) => current.filter((item) => item !== path))}
      onBrowseContext={() => {
        if (!session) return;
        void agentHubChooseFiles(session.projectPath).then((picked) => {
          setAttachments((current) => [...current, ...picked.filter((path) => !current.includes(path))]);
        }).catch((e) => setError(errorMessage(e)));
      }}
      configOptions={transcriptModel.configOptions}
      mode={transcriptModel.mode}
      onSetMode={(mode) => void setMode(mode)}
      onSetConfigOption={(optionId, value) => void setConfigOption(optionId, value)}
      onOpenFile={(path, line) => void openFile(path, line)}
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
