import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentRuntimeQueryKey, agentRuntimeStatus, runtimeBlocked } from "../../../bridge/agentRuntime";
import { agentHubChooseDirectory, agentHubConfigCatalog, CLAUDE_VENDOR, type AgentSession } from "../../../bridge/agenthub";
import { codeGraphProjectRoot } from "../../../bridge/codegraph";
import { useAgentPinsStore } from "../../../stores/agentPins";
import { useModuleFolder } from "../../../stores/workFolder";
import { useModuleVisibility } from "@/components/ui/module-visibility";
import { vendorInfo } from "../vendors";
import { draftConfigOptions, initialModelConfig } from "../modelSettings";
import { openNewAgentSession } from "../documents";
import type { AgentSessionStatus } from "./useAgentSessionStatus";

/**
 * The draft a tab keeps when no session is open yet: which folder it will work
 * in, which agent, and what has been typed — plus the two queries that decide
 * whether that agent can start at all (`draftRuntime`) and what it offers
 * (`draftCatalog`). A vendor may read its catalog in a short-lived process
 * before the conversation exists, without creating one.
 *
 * What the draft attaches — files, images, references — is chips in
 * `prompt` itself (composerDraft.ts), so it goes when the draft is sent or
 * left, with nothing to keep in step.
 */
export function useAgentDraft({ status, tabSessionId }: { status: AgentSessionStatus; tabSessionId: string }) {
  const visible = useModuleVisibility();
  // Where the app is working, for a new draft to start in.
  const { folder: workFolder } = useModuleFolder("agenthub");

  const [draft, setDraft] = useState<{
    cwd: string;
    workspaceId: string;
    parent: AgentSession | null;
    // The rest of a workspace's services, when "grant every service" was
    // picked instead of one folder — see chooseWorkspaceForDraft.
    extraReadWritePaths: string[];
  } | null>(null);
  const [draftAgent, setDraftAgentState] = useState<string>(CLAUDE_VENDOR);
  const [draftPrompt, setDraftPrompt] = useState("");
  const [draftSelections, setDraftSelections] = useState<Record<string, Record<string, string>>>({});

  // Asked while the draft has an agent picked, not after a send fails: the
  // draft shows the install panel as soon as an agent that cannot start is
  // chosen.
  const draftVendor = vendorInfo(draftAgent);
  const draftRuntime = useQuery({
    queryKey: agentRuntimeQueryKey(draftVendor.runtimeId),
    queryFn: () => agentRuntimeStatus(draftVendor.runtimeId),
    enabled: draft !== null && draftVendor.runtimeId !== "",
  });
  const draftCatalog = useQuery({
    queryKey: ["agenthub", "configCatalog", draftAgent, draftRuntime.data?.current?.version],
    queryFn: () => agentHubConfigCatalog(draftAgent),
    enabled: visible && draft !== null && draftVendor.configCatalog === true && Boolean(draftRuntime.data && !runtimeBlocked(draftRuntime.data)),
    staleTime: 60_000,
    retry: false,
  });
  const draftOptions = draftConfigOptions(draftCatalog.data ?? [], draftSelections[draftAgent] ?? {});
  // What stands between a draft and its first message, in the draft's own view.
  const draftBlocked =
    draftVendor.runtimeId !== "" && Boolean(draftRuntime.data && runtimeBlocked(draftRuntime.data));

  /** Starts a draft in a folder that is already known — a project row, a parent. */
  function startDraft(cwd: string, parent: AgentSession | null, workspaceId = "") {
    setDraft({ cwd, workspaceId: parent?.workspaceId || workspaceId, parent, extraReadWritePaths: [] });
    setDraftPrompt("");
    status.resetAll();
  }

  /**
   * "Grant every service" in the draft's picker: cwd anchors on the first
   * one (a concrete WorkDir the sandbox can bind), the rest become
   * ExtraReadWritePaths — a workspace's services do not necessarily share a
   * root cwd alone could cover (docs/dev-kit/workspace.mdx).
   */
  function chooseWorkspaceForDraft(workspaceId: string, paths: string[]) {
    if (paths.length === 0) return;
    const [cwd, ...rest] = paths;
    setDraft((current) => current && { ...current, cwd, workspaceId, extraReadWritePaths: rest });
  }

  /**
   * Starts a draft straight away, from "+ New", ⌘N and the empty state. The
   * folder is the one the app is working in, else the last one used, else where
   * Code Graph is looking, else none; the draft's project picker changes it
   * before the first message.
   */
  async function startNewDraft() {
    const recent = useAgentPinsStore.getState().recentProjects[0];
    const cwd = workFolder || recent || (await codeGraphProjectRoot().catch(() => ""));
    startDraft(cwd, null);
  }

  /** "+ New", ⌘N, the empty state: drafted here in the new-session tab, else in it. */
  function newSession() {
    if (tabSessionId) openNewAgentSession();
    else void startNewDraft();
  }

  /** "New project…" in the draft's picker: the native folder dialog. */
  async function chooseFolderForDraft() {
    // Opens where the draft already points, else the folder the app is working in.
    const start = draft?.cwd || workFolder || (await codeGraphProjectRoot().catch(() => ""));
    try {
      const picked = await agentHubChooseDirectory(start);
      // "" is a cancelled picker: the draft keeps the folder it had. Picking
      // one folder narrows back from "whole workspace" if that was chosen.
      if (picked) setDraft((current) => current && { ...current, cwd: picked, workspaceId: "", extraReadWritePaths: [] });
    } catch (e) {
      status.reportError(e);
    }
  }

  /** The draft's project picker: another folder, no longer a whole workspace. */
  function setDraftProject(path: string) {
    setDraft((current) => current && { ...current, cwd: path, workspaceId: "", extraReadWritePaths: [] });
  }

  /** A workspace chosen without its services: keep every folder saved with it. */
  function chooseWorkspaceContext(workspaceId: string) {
    setDraft((current) => current && { ...current, workspaceId, extraReadWritePaths: [] });
  }

  function setDraftAgent(agent: string) {
    setDraftAgentState(agent);
    // A sign-in waiting for one agent is not the next one's: the panel would
    // name one vendor and sign in to the other.
    status.setAuthRetry(null);
  }

  /** A model or effort picked in the draft: kept until the session exists. */
  function setDraftConfigOption(optionId: string, value: string) {
    setDraftSelections((previous) => ({
      ...previous,
      [draftAgent]: initialModelConfig(draftConfigOptions(
        draftCatalog.data ?? [], { ...previous[draftAgent], [optionId]: value },
      )),
    }));
  }

  /** The draft became a session (or was left): nothing of it is kept. */
  function clearDraft() {
    setDraft(null);
    setDraftPrompt("");
  }

  return {
    draft,
    draftAgent,
    draftPrompt,
    draftVendor,
    draftRuntime,
    draftCatalog,
    draftOptions,
    draftBlocked,
    startDraft,
    chooseWorkspaceForDraft,
    startNewDraft,
    newSession,
    chooseFolderForDraft,
    setDraftProject,
    chooseWorkspaceContext,
    setDraftAgent,
    setDraftPrompt,
    setDraftConfigOption,
    clearDraft,
  };
}

export type AgentDraft = ReturnType<typeof useAgentDraft>;
