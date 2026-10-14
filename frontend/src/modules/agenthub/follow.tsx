import * as React from "react";
import { Eye, X } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { EMPTY_ARRAY } from "@/lib/stableEmpty";
import { useAgentPinsStore } from "@/stores/agentPins";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { dispatchIntent, type EntityRef } from "@/workbench/intents";
import { hasEntityPreview, renderEntityPreview, toolActivityOf } from "@/workbench/toolActivity";
import type { TranscriptItem } from "./types";
import { AgentToolActivity, activityTargetOf, isToolActivity } from "./transcript/AgentToolActivity";
import { toolNameOf } from "@/workbench/toolPayload";
import { AgentRunningCommand, isRunningCommand } from "./transcript/AgentRunningCommand";
import { toolActivityState } from "./transcript/AgentToolCall";

/** Whether the person follows this session (stores/agentPins). */
export function useFollowing(sessionId: string | undefined): [boolean, () => void] {
  const on = useAgentPinsStore((s) => Boolean(sessionId) && s.following.includes(sessionId!));
  const toggle = useAgentPinsStore((s) => s.toggleFollowing);
  return [on, React.useCallback(() => sessionId && toggle(sessionId), [sessionId, toggle])];
}

/** The intent that opens a ref: `<module>.reveal`, API client's is `api.`. */
export function revealIntent(module: string): string {
  return module === "apiclient" ? "api.reveal" : `${module}.reveal`;
}

/** What a session changed in DevKit, and the commands it is still running, newest first. */
export function activityFeed(items: readonly TranscriptItem[]): TranscriptItem[] {
  const out: TranscriptItem[] = [];
  for (let i = items.length - 1; i >= 0; i -= 1) {
    const item = items[i];
    if (isToolActivity(item.toolCall) || isRunningCommand(item)) out.push(item);
  }
  return out;
}

/**
 * The newest finished change whose target passes `accept` — by default any
 * that can be opened. The preview asks for one its module can draw, so a board
 * made after a task does not take the task's live view away.
 */
export function latestTarget(
  feed: readonly TranscriptItem[],
  accept: (ref: EntityRef) => boolean = () => true,
): { ref: EntityRef; label: string } | null {
  for (const item of feed) {
    const call = item.toolCall;
    const activity = call ? toolActivityOf(toolNameOf(call)) : undefined;
    if (!call || !activity || toolActivityState(call.status) !== "done") continue;
    const target = activityTargetOf(activity, call, "done");
    if (target?.ref && accept(target.ref)) return { ref: target.ref, label: target.detail || target.label };
  }
  return null;
}

/**
 * How many of the newest cards the panel draws, and how many more each "Show
 * earlier" adds. The feed is every change the session ever made in DevKit: a
 * long one is hundreds of cards, all mounted at once when the panel opens and
 * all walked again on every streamed token, so only the newest are drawn.
 */
export const FOLLOW_FEED_PAGE = 40;

/** The newest `limit` cards of a feed (it is newest first), and how many are left out. */
export function feedWindow<T>(feed: readonly T[], limit: number): { drawn: readonly T[]; hidden: number } {
  const drawn = feed.length > limit ? feed.slice(0, limit) : feed;
  return { drawn, hidden: feed.length - drawn.length };
}

/**
 * Agent Hub's activity panel, in the module's own right-hand pane while the
 * person follows a session: what the agent is changing in DevKit as it happens
 * — the card of the call under way at the top — and, below, a live view of
 * the last thing it made or changed, drawn by the module that owns it
 * (registerEntityPreview). A task is its detail, updating as the agent moves
 * it; anything without a view of its own keeps its card and Open.
 *
 * It reads the transcript itself, so the pane is handed one element per
 * session rather than a new one on every streamed token.
 */
export function AgentActivityPanel({ sessionId, title, onStop }: { sessionId: string; title: string; onStop: () => void }) {
  const items = useAgentSessionsStore((s) => s.transcriptsById[sessionId]?.items ?? EMPTY_ARRAY);
  const feed = React.useMemo(() => activityFeed(items), [items]);
  // Keyed to the session so following another one starts from its newest again.
  const [extra, setExtra] = React.useState({ sessionId, count: 0 });
  const limit = FOLLOW_FEED_PAGE + (extra.sessionId === sessionId ? extra.count : 0);
  const { drawn, hidden } = React.useMemo(() => feedWindow(feed, limit), [feed, limit]);
  const latest = React.useMemo(() => latestTarget(feed, hasEntityPreview), [feed]);
  // Keyed by what the preview is OF, not the object: the feed is rebuilt on
  // every streamed token, and the embedded view must not be with it.
  const previewKey = latest ? `${latest.ref.module}\u0000${latest.ref.kind}\u0000${latest.ref.id}` : "";
  const preview = React.useMemo(() => {
    if (!previewKey) return null;
    const [module, kind, id] = previewKey.split("\u0000");
    return renderEntityPreview({ module, kind, id, label: "" });
  }, [previewKey]);

  return (
    <section data-testid="agent-activity-panel" aria-label="Agent activity" className="flex h-full min-h-0 w-full min-w-0 flex-col">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
        <Eye className="size-3.5 shrink-0 text-icon-secondary" aria-hidden="true" />
        <span className="min-w-0 flex-1 truncate text-label-12 text-text-secondary" title={title}>
          {`Following ${title}`}
        </span>
        <Button variant="ghost" size="icon-xs" aria-label="Stop following" title="Stop following" onClick={onStop}>
          <X />
        </Button>
      </header>
      {feed.length === 0 ? (
        <EmptyState
          size="rail"
          className="flex-1"
          title="Nothing changed yet"
          body="Boards, tasks, notes, commits and requests this agent makes show up here as it makes them."
        />
      ) : (
        <>
          <div
            data-testid="agent-activity-feed"
            className={preview ? "flex max-h-[45%] shrink-0 flex-col gap-2 overflow-y-auto p-3" : "flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-3"}
          >
            {drawn.map((item) =>
              isRunningCommand(item) ? (
                <AgentRunningCommand key={item.id} item={item} />
              ) : (
                <AgentToolActivity key={item.id} call={item.toolCall!} sessionId={item.sessionId ?? sessionId} />
              ),
            )}
            {hidden > 0 ? (
              <Button
                variant="ghost"
                size="sm"
                data-testid="agent-activity-more"
                onClick={() => setExtra({ sessionId, count: limit })}
              >
                {`Show ${Math.min(hidden, FOLLOW_FEED_PAGE)} earlier`}
              </Button>
            ) : null}
          </div>
          {preview && latest ? (
            <div data-testid="agent-activity-preview" className="flex min-h-0 flex-1 flex-col border-t border-border-subtle">
              <p className="shrink-0 px-3 pt-2 text-caption-11-medium text-text-tertiary">{`Latest · ${latest.label}`}</p>
              <div key={`${latest.ref.module}:${latest.ref.id}`} className="activity-enter min-h-0 flex-1 overflow-y-auto">
                <React.Suspense fallback={null}>{preview}</React.Suspense>
              </div>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}

/**
 * Following from outside Agent Hub (the Agent panel): no pane of its own to
 * fill, so each finished change is a toast with Open. Only changes that finish
 * from now on — a session's history does not replay as a burst of toasts.
 */
export function useFollowToasts(sessionId: string, items: readonly TranscriptItem[], enabled: boolean): void {
  const seen = React.useRef<Set<string> | null>(null);
  React.useEffect(() => {
    seen.current = null;
  }, [sessionId, enabled]);
  React.useEffect(() => {
    if (!enabled || !sessionId) return;
    const done = items.filter((item) => {
      const call = item.toolCall;
      return isToolActivity(call) && toolActivityState(call!.status) === "done";
    });
    if (!seen.current) {
      seen.current = new Set(done.map((item) => item.id));
      return;
    }
    for (const item of done) {
      if (seen.current.has(item.id)) continue;
      seen.current.add(item.id);
      const call = item.toolCall!;
      const activity = toolActivityOf(toolNameOf(call))!;
      const target = activityTargetOf(activity, call, "done");
      const ref = target?.ref;
      toast(target?.label ? `${activity.done} · ${target.label}` : activity.done, {
        id: `follow:${item.id}`,
        action: ref ? { label: "Open", onClick: () => void dispatchIntent(revealIntent(ref.module), ref, { placement: "beside" }) } : undefined,
      });
    }
  }, [items, sessionId, enabled]);
}
