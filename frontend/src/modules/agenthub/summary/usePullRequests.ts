import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { attachPullRequest, detachPullRequest, onPullRequestsChanged, pullRequestState } from "@/bridge/pullRequests";
import { toAppError } from "@/bridge/errors";
import { EMPTY_PULL_REQUESTS, folderName, pullRequestsView, type PullRequestCard, type PullRequestsView } from "./pullRequestModel";

/** A view plus what the person can do to it. */
export type PullRequestsPanel = PullRequestsView & {
  /** Rejects with the reason (no account, not found, not an address). */
  attach?: (url: string) => Promise<void>;
  detach?: (card: PullRequestCard) => Promise<void>;
};

export const EMPTY_PANEL: PullRequestsPanel = EMPTY_PULL_REQUESTS;

export const pullRequestsKey = (sessionId: string, folders: readonly string[]) =>
  ["agenthub", "pullRequests", sessionId, folders.join("\n")] as const;

type Input = {
  sessionId: string;
  /** The repositories this work spans (the composer tray already read them). */
  folders: readonly string[];
  /** The agent is running a turn. When it ends the branch may have a request now. */
  live: boolean;
  /** False for a draft, or while the session is not the one on screen. */
  enabled: boolean;
};

/**
 * The pull (or merge) request each repository's branch became, for the summary
 * card. Go reads git and calls the host with the account it chose; this only
 * asks again when Go says to (`nextPollMs`), never while the window is hidden
 * (react-query pauses interval refetches in the background), and at once when
 * a turn ends or a request is attached by pr.create.
 */
export function useSessionPullRequests({ sessionId, folders, live, enabled }: Input): PullRequestsPanel {
  const queryClient = useQueryClient();
  const folderList = React.useMemo(() => [...folders], [folders]);
  const key = React.useMemo(() => pullRequestsKey(sessionId, folderList), [sessionId, folderList]);
  const active = enabled && Boolean(sessionId) && folderList.length > 0;

  const query = useQuery({
    queryKey: key,
    queryFn: () => pullRequestState(sessionId, folderList),
    enabled: active,
    // Go's answer to "when": quick while a check runs, slow once settled, never
    // for a merged request. A failed call keeps the last card and tries again.
    refetchInterval: (q) => {
      const ms = q.state.data?.nextPollMs ?? -1;
      return ms > 0 ? ms : false;
    },
    refetchIntervalInBackground: false,
    staleTime: 5_000,
    retry: false,
  });

  React.useEffect(() => {
    if (!active) return;
    return onPullRequestsChanged((id) => {
      if (id === sessionId) void queryClient.invalidateQueries({ queryKey: key });
    });
  }, [active, queryClient, key, sessionId]);

  const wasLive = React.useRef(live);
  React.useEffect(() => {
    if (wasLive.current && !live && active) void queryClient.invalidateQueries({ queryKey: key });
    wasLive.current = live;
  }, [live, active, queryClient, key]);

  const view = React.useMemo(() => (active ? pullRequestsView(query.data?.items) : EMPTY_PULL_REQUESTS), [active, query.data]);

  const attach = React.useCallback(
    async (url: string) => {
      try {
        await attachPullRequest(sessionId, folderList[0] ?? "", url.trim());
      } catch (error) {
        throw new Error(toAppError(error).message);
      }
      await queryClient.invalidateQueries({ queryKey: key });
    },
    [sessionId, folderList, queryClient, key],
  );
  const detach = React.useCallback(
    async (card: PullRequestCard) => {
      const [host, ...rest] = card.key.replace(/#\d+$/, "").split("/");
      await detachPullRequest(sessionId, host, rest.join("/"), card.number);
      await queryClient.invalidateQueries({ queryKey: key });
    },
    [sessionId, queryClient, key],
  );

  return React.useMemo(() => (active ? { ...view, attach, detach } : EMPTY_PANEL), [active, view, attach, detach]);
}

export { folderName };
