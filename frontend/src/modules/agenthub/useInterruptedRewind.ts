import { useCallback, useEffect, useRef, useState } from "react";

import { agentHubListCheckpoints, onAgentHubSessionRewound } from "@/bridge/agenthub";
import type { CheckpointSummary } from "../../../bindings/devkit/internal/agenthub/models";
import { selectInterruptedRewind, type InterruptedRewind } from "./interruptedRewind";

export type InterruptedRewindSources = {
  list: (sessionId: string) => Promise<CheckpointSummary[] | null | undefined>;
  onRewound: (handler: (sessionId: string) => void) => () => void;
};

const LIVE: InterruptedRewindSources = {
  list: agentHubListCheckpoints,
  onRewound: onAgentHubSessionRewound,
};

/**
 * Follows one session's interrupted rewind.
 *
 * Reads the session's own checkpoint list (not the global `InterruptedRewinds`, which has no
 * session id) and reads it again when asked and when this session is rewound. Every read
 * carries a generation: an answer that was overtaken by a newer read, or that arrives after
 * `stop`, is dropped, so a slow answer never brings back a row that was settled meanwhile or
 * shows another session's row. A read that fails keeps what is on screen (the row's state is
 * then unknown, and "gone" would be a claim); the first read of a session that fails shows
 * nothing.
 */
export function followInterruptedRewind(
  sessionId: string,
  sources: InterruptedRewindSources,
  setItem: (next: InterruptedRewind | null) => void,
): { reload: () => Promise<void>; stop: () => void } {
  let stopped = false;
  let generation = 0;
  const reload = async () => {
    const mine = ++generation;
    try {
      const records = await sources.list(sessionId);
      if (!stopped && mine === generation) setItem(selectInterruptedRewind(records));
    } catch {
      // Keep what is shown. Nothing was learned.
    }
  };
  const initial = (async () => {
    const mine = ++generation;
    try {
      const records = await sources.list(sessionId);
      if (!stopped && mine === generation) setItem(selectInterruptedRewind(records));
    } catch {
      if (!stopped && mine === generation) setItem(null);
    }
  })();
  void initial;
  const off = sources.onRewound((eventSessionId) => {
    if (!stopped && eventSessionId === sessionId) void reload();
  });
  return {
    reload,
    stop: () => {
      stopped = true;
      off();
    },
  };
}

export type InterruptedRewindHandle = {
  item: InterruptedRewind | null;
  reload: () => Promise<void>;
};

/**
 * The interrupted rewind of a session, or null.
 *
 * `suspended` is for the moments a rewind may be running (its dialog is open): a rewind in
 * flight holds a pending row too, and that is not an interruption. When it ends the list is
 * read again, so a rewind that stopped part-way raises the notice as soon as its dialog closes.
 */
export function useInterruptedRewind(
  sessionId: string,
  options: { enabled?: boolean; suspended?: boolean } = {},
): InterruptedRewindHandle {
  const enabled = (options.enabled ?? true) && Boolean(sessionId);
  const suspended = options.suspended ?? false;
  const [item, setItem] = useState<InterruptedRewind | null>(null);
  const reloadRef = useRef<() => Promise<void>>(async () => {});

  useEffect(() => {
    // A previous session's row must not show under this one for the time the read takes.
    setItem(null);
    if (!enabled) {
      reloadRef.current = async () => {};
      return;
    }
    const follow = followInterruptedRewind(sessionId, LIVE, setItem);
    reloadRef.current = follow.reload;
    return () => {
      reloadRef.current = async () => {};
      follow.stop();
    };
  }, [enabled, sessionId]);

  // The rewind dialog just closed: whatever it left behind is in the list now.
  const wasSuspended = useRef(suspended);
  useEffect(() => {
    if (wasSuspended.current && !suspended) void reloadRef.current();
    wasSuspended.current = suspended;
  }, [suspended]);

  const reload = useCallback(() => reloadRef.current(), []);
  return { item: suspended ? null : item, reload };
}
