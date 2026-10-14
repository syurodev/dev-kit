import { useEffect, useState } from "react";

import {
  agentHubCheckpointFileDiff,
  agentHubListCheckpoints,
  onAgentHubCheckpoint,
  onAgentHubSessionRewound,
} from "@/bridge/agenthub";
import type { CheckpointSummary, FileDiff } from "../../../bindings/devkit/internal/agenthub/models";
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
    // Reset first: the previous session's checkpoints must not show under this
    // one's turns for the time the load takes.
    setIndex(EMPTY_CHECKPOINTS);
    return followSessionCheckpoints(sessionId, LIVE, setIndex);
  }, [enabled, sessionId]);

  return index;
}

type CheckpointSources = {
  list: (sessionId: string) => Promise<CheckpointSummary[] | null | undefined>;
  onCheckpoint: (handler: (sessionId: string, record: CheckpointSummary | null | undefined) => void) => () => void;
  onRewound: (handler: (sessionId: string) => void) => () => void;
};

const LIVE: CheckpointSources = {
  list: agentHubListCheckpoints,
  onCheckpoint: onAgentHubCheckpoint,
  onRewound: onAgentHubSessionRewound,
};

/**
 * Lists a session's checkpoints, applies each live one, and lists them again
 * after a rewind: a rewind deletes the turns after the point it went back to,
 * and no checkpoint event says so. Returns the unsubscribe.
 */
export function followSessionCheckpoints(
  sessionId: string,
  sources: CheckpointSources,
  setIndex: (next: CheckpointIndex | ((previous: CheckpointIndex) => CheckpointIndex)) => void,
): () => void {
  let cancelled = false;
  let generation = 0;
  const load = () => {
    const mine = ++generation;
    sources
      .list(sessionId)
      .then((records) => {
        if (!cancelled && mine === generation) setIndex(indexCheckpoints(records));
      })
      .catch(() => {
        // Checkpoints are an extra; a session whose list cannot be read still
        // shows its transcript, just without the counts.
        if (!cancelled && mine === generation) setIndex(EMPTY_CHECKPOINTS);
      });
  };
  load();
  const offCheckpoint = sources.onCheckpoint((eventSessionId, record) => {
    if (cancelled || eventSessionId !== sessionId) return;
    setIndex((previous) => applyCheckpoint(previous, record));
  });
  const offRewound = sources.onRewound((eventSessionId) => {
    if (!cancelled && eventSessionId === sessionId) load();
  });
  return () => {
    cancelled = true;
    offCheckpoint();
    offRewound();
  };
}

/** One changed file's two versions, for the drawer under it. */
export function loadCheckpointFileDiff(checkpointId: string, path: string): Promise<FileDiff> {
  return agentHubCheckpointFileDiff(checkpointId, path);
}
