import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  agentHubCancel,
  agentHubCloseSession,
  agentHubDeleteSession,
  agentHubGetStatus,
  agentHubScratchRoot,
  type AgentSession,
} from "@/bridge/agenthub";
import { copyText } from "@/bridge/clipboard";
import { errorMessage } from "@/bridge/errors";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionList, useAgentSessionsStore } from "@/stores/agentSessions";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { AgentSessionSidebar } from "./sidebar/AgentSessionSidebar";
import { attentionBySession } from "./sidebar/model";
import { RenameSessionDialog } from "./surfaces/RenameSessionDialog";
import { showSubagent } from "./sidebar/openSubagent";
import { useSidebarWorkspaces } from "./sidebarWorkspaces";
import { forgetAgentTranscript, loadWholeTranscript } from "./transcript";
import { transcriptToMarkdown } from "./exportMarkdown";
import { groupTurns } from "./turns";
import { renameAgentSession } from "./AgentHubView";
import {
  AGENTHUB_MODULE,
  activeAgentHubSessionId,
  closeAgentSessionTabs,
  openAgentSession,
  openNewAgentSession,
  sessionTitle,
} from "./documents";

function reportError(e: unknown) {
  toast.error(errorMessage(e));
}

/** Every descendant of `root` in `sessions`, and `root` itself. */
export function sessionSubtree(sessions: AgentSession[], root: string): Set<string> {
  const gone = new Set<string>([root]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const s of sessions) {
      if (!gone.has(s.id) && s.parentSessionId && gone.has(s.parentSessionId)) {
        gone.add(s.id);
        grew = true;
      }
    }
  }
  return gone;
}

/**
 * Agent Hub's rail: sessions in collapsible groups, as the module's rather than
 * a document's (ModuleManifest.sidebar). Each session is a tab, and a rail
 * registered by every open tab would belong to whichever registered last; this
 * one is there with no tab open as well. A card opens its session's tab, and the
 * highlighted card is the tab Agent Hub's focused group shows. Closing a tab
 * never closes the session: that is the card's menu.
 *
 * This is the container: it reads the stores, runs the actions and owns the
 * rename / delete dialogs, which show only while Agent Hub is the module on
 * screen. The presentation is AgentSessionSidebar.
 */
export function AgentHubSidebar() {
  const sessions = useAgentSessionList();
  const activeSessionId = useWorkbenchTabsStore(activeAgentHubSessionId);
  const pendingQueue = useAgentSessionsStore((state) => state.pendingQueue);
  const elicitationQueue = useAgentSessionsStore((state) => state.elicitationQueue);
  const subagents = useAgentSessionsStore((state) => state.subagents);
  const upsertSession = useAgentSessionsStore((state) => state.upsertSession);
  const removeSession = useAgentSessionsStore((state) => state.removeSession);
  const dequeuePendingForSession = useAgentSessionsStore((state) => state.dequeuePendingForSession);
  const dequeueElicitationForSession = useAgentSessionsStore((state) => state.dequeueElicitationForSession);
  const pinned = useAgentPinsStore((state) => state.pinned);
  const togglePin = useAgentPinsStore((state) => state.togglePin);
  const unpin = useAgentPinsStore((state) => state.unpin);
  const collapsed = useAgentPinsStore((state) => state.collapsed);
  const toggleCollapsed = useAgentPinsStore((state) => state.toggleCollapsed);
  const olderOpen = useAgentPinsStore((state) => state.olderOpen);
  const toggleOlderOpen = useAgentPinsStore((state) => state.toggleOlderOpen);
  const hideClosed = useAgentPinsStore((state) => state.hideClosed);
  const toggleHideClosed = useAgentPinsStore((state) => state.toggleHideClosed);
  const workspaces = useSidebarWorkspaces();
  // The rail's dialogs belong to the module on screen, not to a rail left
  // mounted behind another one. A module's rail has no visibility provider of
  // its own (SidebarSurface keeps visited rails mounted, hidden), so it reads
  // the active module.
  const visible = useWorkbenchTabsStore((state) => state.activeModuleId === AGENTHUB_MODULE);
  // Where "no project" sessions get their folders. Read once: it is a fixed
  // place under the config dir, and the sidebar groups everything under it.
  const scratchRoot =
    useQuery({ queryKey: ["agenthub", "scratchRoot"], queryFn: agentHubScratchRoot, staleTime: Infinity }).data ?? "";

  // The row the sidebar asked to rename, and the one awaiting a delete
  // confirmation with how many go with it.
  const [pendingRename, setPendingRename] = useState<AgentSession | null>(null);
  const [pendingDelete, setPendingDelete] = useState<{ session: AgentSession; descendants: number } | null>(null);

  // What each session is waiting on the user for: permissions and questions
  // alike, since either one stops the agent until someone answers. Kept apart
  // so a card can say which.
  const attention = useMemo(() => attentionBySession(pendingQueue, elicitationQueue), [pendingQueue, elicitationQueue]);

  async function cancelTurn(session: AgentSession) {
    try {
      await agentHubCancel(session.id);
      dequeuePendingForSession(session.id);
      dequeueElicitationForSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      reportError(e);
    }
  }

  async function closeSession(session: AgentSession) {
    try {
      await agentHubCloseSession(session.id);
      upsertSession(await agentHubGetStatus(session.id));
    } catch (e) {
      reportError(e);
    }
  }

  // Every page, not the window on screen: a copy that stopped where the scroll
  // did would be the newest half of a conversation with no sign it was half.
  async function copyTranscript(session: AgentSession) {
    try {
      const model = await loadWholeTranscript(session.id);
      await copyText(
        transcriptToMarkdown(model.items, {
          title: session.title,
          projectPath: session.projectPath,
          vendor: session.agentType,
        }),
      );
      const turns = groupTurns(model.items).length;
      toast.success(`Copied ${turns} turn${turns === 1 ? "" : "s"} as Markdown`);
    } catch (e) {
      reportError(e);
    }
  }

  async function deleteConfirmed() {
    const target = pendingDelete?.session;
    setPendingDelete(null);
    if (!target) return;
    try {
      await agentHubDeleteSession(target.id);
      // The backend takes the subtree, so the store has to as well — a child
      // left in the list would render as a root pointing at a session that no
      // longer exists. Their tabs go with them: a tab for a deleted session
      // has nothing to show.
      const gone = sessionSubtree(sessions, target.id);
      closeAgentSessionTabs(gone);
      for (const id of gone) {
        removeSession(id);
        forgetAgentTranscript(id);
        unpin(id);
      }
      const agents = useAgentSessionsStore.getState();
      if (gone.has(agents.activeSessionId)) agents.setActiveSessionId("");
    } catch (e) {
      reportError(e);
    }
  }

  return (
    <>
      <AgentSessionSidebar
        sessions={sessions}
        workspaces={workspaces}
        scratchRoot={scratchRoot}
        activeSessionId={activeSessionId}
        attention={attention}
        subagents={subagents}
        pinnedIds={pinned}
        onTogglePin={togglePin}
        collapsedKeys={collapsed}
        onToggleCollapsed={toggleCollapsed}
        olderOpenKeys={olderOpen}
        onToggleOlder={toggleOlderOpen}
        hideClosed={hideClosed}
        onToggleHideClosed={toggleHideClosed}
        onSelectSession={(id) => {
          const session = sessions.find((s) => s.id === id);
          if (session) openAgentSession(session);
        }}
        // A subagent's work, beside its session: the session's tab, and the
        // module's right-hand pane on that subagent (subagents.tsx).
        onSelectSubagent={showSubagent}
        onNewSession={() => openNewAgentSession()}
        // The "No project" group's + starts another scratch session, not one
        // in the scratch root itself. A workspace's + is the whole workspace:
        // its primary as cwd, the other services granted beside it.
        onNewSessionIn={(projectPath, workspaceId, workspacePaths) =>
          openNewAgentSession({
            cwd: projectPath === scratchRoot ? "" : projectPath,
            defaultFolder: false,
            workspaceId,
            workspacePaths,
          })
        }
        onNewChildSession={(parent) =>
          openNewAgentSession({ cwd: parent.projectPath, defaultFolder: false, parent })
        }
        onCancelTurn={(session) => void cancelTurn(session)}
        onCloseSession={(session) => void closeSession(session)}
        onRenameSession={setPendingRename}
        onCopyTranscript={(session) => void copyTranscript(session)}
        onDeleteSession={(session, descendants) => setPendingDelete({ session, descendants })}
        onCopy={(text) => void copyText(text)}
      />

      <RenameSessionDialog
        initialTitle={visible && pendingRename ? sessionTitle(pendingRename) : null}
        // Not caught: the dialog shows the reason and stays open.
        onRename={(title) => (pendingRename ? renameAgentSession(pendingRename.id, title) : undefined)}
        onClose={() => setPendingRename(null)}
      />

      {/* Delete is the one action here with nothing behind it: close keeps the
          row as history, this removes the transcript for good. */}
      <AlertDialog
        open={visible && Boolean(pendingDelete)}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{`Delete ${pendingDelete?.session.title || "this session"}?`}</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.descendants
                ? `Its transcript and ${pendingDelete.descendants} child session${
                    pendingDelete.descendants === 1 ? "" : "s"
                  } will be deleted for good. Any of them still running will be stopped. This cannot be undone.`
                : "Its transcript will be deleted for good, and the agent stopped if it is still running. This cannot be undone."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void deleteConfirmed()}>Delete</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
