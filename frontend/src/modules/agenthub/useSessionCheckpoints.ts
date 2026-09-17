import { useEffect, useState } from "react";

import { agentHubCheckpointFileDiff, agentHubListCheckpoints, onAgentHubCheckpoint } from "@/bridge/agenthub";
import type { FileDiff } from "../../../bindings/devkit/internal/agenthub/models";
import { applyCheckpoint, EMPTY_CHECKPOINTS, indexCheckpoints, type CheckpointIndex } from "./checkpoints";

/**
 * A session's file checkpoints, indexed by the prompt that started each turn.
 *
 * Its own hook rather than part of the transcript store: a checkpoint is
 * settled after its turn has ended and can be rewritten later still, when the
 * agent's own report of what it changed arrives late. Folding that into the
 * transcript's paging would make a late correction look like a new transcript
 * row.
 */
export function useSessionCheckpoints(sessionId: string, options: { enabled?: boolean } = {}): CheckpointIndex {
  const enabled = options.enabled ?? true;
  const [index, setIndex] = useState<CheckpointIndex>(EMPTY_CHECKPOINTS);

  useEffect(() => {
    if (!enabled || !sessionId) {
      setIndex(EMPTY_CHECKPOINTS);
      return;
    }
    let cancelled = false;
    // Reset first: the previous session's checkpoints must not show under this
    // one's turns for the time the load takes.
    setIndex(EMPTY_CHECKPOINTS);
    agentHubListCheckpoints(sessionId)
      .then((records) => {
        if (!cancelled) setIndex(indexCheckpoints(records));
      })
      .catch(() => {
        // Checkpoints are an extra; a session whose list cannot be read still
        // shows its transcript, just without the counts.
        if (!cancelled) setIndex(EMPTY_CHECKPOINTS);
      });

    const off = onAgentHubCheckpoint((eventSessionId, record) => {
      if (cancelled || eventSessionId !== sessionId) return;
      setIndex((previous) => applyCheckpoint(previous, record));
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [enabled, sessionId]);

  return index;
}

/** One changed file's two versions, for the drawer under it. */
export function loadCheckpointFileDiff(checkpointId: string, path: string): Promise<FileDiff> {
  return agentHubCheckpointFileDiff(checkpointId, path);
}
