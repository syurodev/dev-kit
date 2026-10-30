import * as React from "react";
import { Bot, Square } from "lucide-react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";

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
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import type { PanelHeader } from "@/components/ui/split-panel";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { cancelSessionTurn } from "./session/cancelTurn";
import { isWorking } from "./sessionActivity";
import { findSubagent, isSubagentRunning, subagentStatus } from "./subagents";
import { AgentState } from "./surfaces/AgentState";
import type { Subagent, TranscriptItem } from "./types";

const NO_FACTS: { name?: string; task?: string; state?: string } = {};

/** Every row a subagent appears in, folded: one walk per transcript, however many pieces of the header ask. */
const folded = new WeakMap<readonly TranscriptItem[], Map<string, Subagent | null>>();
function seenSubagent(items: readonly TranscriptItem[], id: string): Subagent | null {
  let byId = folded.get(items);
  if (!byId) folded.set(items, (byId = new Map()));
  if (!byId.has(id)) byId.set(id, findSubagent(items, id));
  return byId.get(id)!;
}

/**
 * A subagent's name, task and state, as the pane's header reads them: the
 * tracked subagent over what the transcript's rows say of it (SubagentPanel
 * merges the same two). Primitives only, so a streamed token that changes
 * neither does not re-render the header; and the walk of the transcript is
 * once per change of it, not once per store update, and none while nothing is
 * open (`subagentId` empty: the opener calls this unconditionally).
 */
function useSubagentFacts(sessionId: string, subagentId: string): { name?: string; task?: string; state?: string } {
  return useAgentSessionsStore(
    useShallow((s) => {
      if (!subagentId) return NO_FACTS;
      const tracked = s.subagents[sessionId]?.find((a) => a.id === subagentId);
      const seen = seenSubagent(s.transcriptsById[sessionId]?.items ?? EMPTY_ARRAY, subagentId);
      const { name, task, state } = { ...seen, ...tracked };
      return { name, task, state };
    }),
  );
}

function SubagentStatus({ sessionId, subagentId }: { sessionId: string; subagentId: string }) {
  const { state } = useSubagentFacts(sessionId, subagentId);
  return <AgentState status={subagentStatus(state)} />;
}

/**
 * There is no per-subagent cancel in DevKit: the session's runtime is one
 * process and `Cancel` ends all of it — the turn, every subagent and every
 * background task. So the control says that, and asks before it does it. The
 * same function as the sidebar's "Cancel turn", offered only where there is a
 * turn to end: the subagent still runs and the session is working.
 */
function SubagentStop({ sessionId, subagentId }: { sessionId: string; subagentId: string }) {
  const { state } = useSubagentFacts(sessionId, subagentId);
  const working = useAgentSessionsStore((s) => isWorking(s.sessionsById[sessionId]));
  const shown = Boolean(subagentId) && isSubagentRunning(state) && working;
  const [asking, setAsking] = React.useState(false);
  const busy = React.useRef(false);
  // A question belongs to the turn it was asked about: once there is no turn
  // to stop, it is withdrawn, so it does not come back when the session works
  // again. (Another subagent or session is another element: see the keys in useSubagentHeader.)
  React.useEffect(() => {
    if (!shown) setAsking(false);
  }, [shown]);
  if (!shown) return null;
  // The action stays pressable while the question closes: one cancel, however often it is hit.
  function confirm() {
    if (busy.current) return;
    busy.current = true;
    void cancelSessionTurn(sessionId, (e) => toast.error(errorMessage(e))).finally(() => {
      busy.current = false;
    });
  }
  return (
    <>
      <ActionTooltip label="Stops the whole session's turn, including this subagent and background tasks">
        <Button variant="ghost" size="icon-xs" aria-label="Stop turn" onClick={() => setAsking(true)}>
          <Square />
        </Button>
      </ActionTooltip>
      <AlertDialog open={asking} onOpenChange={setAsking}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Stop this session's turn?</AlertDialogTitle>
            <AlertDialogDescription>
              A subagent cannot be stopped on its own. This ends the whole turn: this subagent, the other subagents and the background tasks of the
              session. The conversation stays.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={confirm}>Stop turn</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/**
 * What the shell's one header says for a subagent's pane (openPanel's
 * `header`): its name, its task as the tooltip, its state and Stop. The state
 * and Stop read the store themselves, so the header handed to the shell
 * changes only with the name and the task, and the pane's body stays put.
 */
export function useSubagentHeader(sessionId: string, subagentId: string): PanelHeader {
  const { name, task } = useSubagentFacts(sessionId, subagentId);
  // One element per subagent: another one is not the same control, and an open question is not carried over to it.
  const key = `${sessionId}:${subagentId}`;
  return React.useMemo(
    () => ({
      title: name || "Subagent",
      // All of the name, which the row may cut, then the task.
      titleHint: task ? `${name || "Subagent"}\n${task}` : name || "Subagent",
      icon: <Bot className="text-icon-secondary" />,
      status: <SubagentStatus key={key} sessionId={sessionId} subagentId={subagentId} />,
      actions: <SubagentStop key={key} sessionId={sessionId} subagentId={subagentId} />,
    }),
    [name, task, sessionId, subagentId, key],
  );
}
