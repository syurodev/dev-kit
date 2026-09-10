import { useEffect, useState } from "react";
import { Bot, MessageSquare, Send, User, X } from "lucide-react";
import { NodeToolbar, Panel, Position, useInternalNode, useStore, useViewport } from "@xyflow/react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import {
  commentCodeGraphPlanNode,
  listCodeGraphPlanNodeComments,
  type CodeGraphPlanComment,
} from "@/bridge/codegraphPlan";
import { errorMessage } from "@/bridge/errors";
import { DOCK_TOP_RESERVE, placeInspector } from "../inspector/placeInspector";

/** "14m ago" — the thread is short-lived, so an absolute time says less. */
function ago(unixSeconds: number): string {
  const s = Math.max(0, Math.round(Date.now() / 1000 - unixSeconds));
  if (s < 60) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

/**
 * Figma `comments/pn-002` (482:8255) and `Agent Panel Content` (482:8322): a
 * plan node's comment thread, anchored to the node on the canvas. Header with
 * the thread glyph and the node's FQN; comments as 22px avatar + caps meta +
 * body; a composer with the primary send square; the delivery note under it.
 *
 * A popover while Detail and Data flow are a panel, and the split is not
 * arbitrary: a comment is short, about the node you are pointing at, and done
 * when you close it — anchoring is the point. A call trace outlives the glance.
 *
 * placeInspector keeps it on screen — it flips sides, picks the vertical align
 * that does not clip, and gives up on anchoring entirely when no side has room.
 */
export function PlanCommentsPopover({
  planNodeId,
  label,
  onClose,
  onCountChange,
}: {
  planNodeId: string;
  label: string;
  onClose: () => void;
  onCountChange?: (n: number) => void;
}) {
  const [thread, setThread] = useState<CodeGraphPlanComment[] | null>(null);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");

  const canvasWidth = useStore((s) => s.width);
  const canvasHeight = useStore((s) => s.height);
  const viewport = useViewport();
  const node = useInternalNode(planNodeId);

  useEffect(() => {
    let live = true;
    setThread(null);
    setDraft("");
    setError("");
    listCodeGraphPlanNodeComments(planNodeId)
      .then((next) => {
        if (!live) return;
        setThread(next);
        onCountChange?.(next.length);
      })
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
    // onCountChange is called, never read as state.
  }, [planNodeId]);

  if (!node || !canvasWidth || !canvasHeight) return null;

  const placement = placeInspector(
    {
      x: node.internals.positionAbsolute.x * viewport.zoom + viewport.x,
      y: node.internals.positionAbsolute.y * viewport.zoom + viewport.y,
      width: (node.measured.width ?? 0) * viewport.zoom,
      height: (node.measured.height ?? 0) * viewport.zoom,
    },
    { width: canvasWidth, height: canvasHeight },
  );

  async function send() {
    const body = draft.trim();
    if (!body) return;
    setSending(true);
    setError("");
    try {
      await commentCodeGraphPlanNode(planNodeId, body);
      setDraft("");
      const next = await listCodeGraphPlanNodeComments(planNodeId);
      setThread(next);
      onCountChange?.(next.length);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setSending(false);
    }
  }

  const card = (
    <div
      style={{ width: placement.width, height: placement.height }}
      className="nowheel nopan flex flex-col overflow-hidden rounded-lg border border-glass-border bg-surface-raised shadow-[0_12px_32px_rgba(0,0,0,0.45)]"
    >
      <div className="flex shrink-0 items-center gap-2 px-5 pt-4 pb-3">
        <MessageSquare size={16} className="shrink-0 text-text-secondary" />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-title-13 text-text-primary">Comments</span>
          <span title={label} className="truncate text-code-11 text-text-tertiary">
            {label}
          </span>
        </span>
        <Button
          variant="ghost"
          size="icon-xs"
          className="size-5.5 shrink-0"
          aria-label="Close comments"
          onClick={onClose}
        >
          <X size={14} />
        </Button>
      </div>
      <div className="h-px shrink-0 bg-border-subtle" />

      <ScrollArea className="min-h-0 flex-1">
        <ul className="flex flex-col gap-4.5 px-5 py-4">
          {thread?.map((c) => {
            const agent = c.author === "agent";
            const Glyph = agent ? Bot : User;
            return (
              <li key={c.commentId} className="flex items-start gap-2.5">
                <span className="flex size-5.5 shrink-0 items-center justify-center rounded-full bg-state-soft">
                  <Glyph size={14} className="text-text-secondary" />
                </span>
                <span className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="flex items-center gap-2 text-label-11-caps">
                    <span className={agent ? "text-(--brand-text)" : "text-text-secondary"}>
                      {agent ? "Agent" : "You"}
                    </span>
                    <span className="text-text-tertiary normal-case">{ago(c.createdAt)}</span>
                  </span>
                  <span className="whitespace-pre-wrap text-body-12 text-text-secondary">{c.body}</span>
                </span>
              </li>
            );
          })}
          {thread?.length === 0 && (
            <li className="text-body-12 text-text-tertiary">No comments on this node yet.</li>
          )}
          {!thread && <li className="text-body-12 text-text-tertiary">Loading…</li>}
        </ul>
      </ScrollArea>

      <div className="flex shrink-0 items-start gap-2 px-5 pb-3">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={2}
          placeholder="Tell the agent what to do differently here…"
          className="min-w-0 flex-1 resize-none rounded-md border border-border bg-state-soft px-3 py-2.25 text-body-12 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
        />
        <Button
          size="icon"
          className={cn("size-8.5 shrink-0", sending && "opacity-60")}
          aria-label={sending ? "Saving…" : "Send"}
          disabled={sending || !draft.trim()}
          onClick={() => void send()}
        >
          <Send size={15} />
        </Button>
      </div>
      {/* Not decoration: a comment here is handed to the agent with the plan
          and steers what gets built. Letting someone believe it is a private
          note would make the feature actively misleading. Delivery into a live
          session is not built, so it says "next time it reads the plan", not
          "sent" — see comments.go. */}
      <p className="shrink-0 px-5 pb-4 text-code-11 text-text-tertiary">
        {error || "Saved with the plan — the agent reads it next time it reads the plan, and it steers the implementation."}
      </p>
    </div>
  );

  if (placement.mode === "docked") {
    // React Flow's own Panel, not a bare absolute div: it is positioned inside
    // the flow's coordinate space and stacked above the canvas for us.
    // DOCK_TOP_RESERVE is the height placeInspector already subtracted so this
    // never lands on CanvasToolbar, which owns the same corner.
    return (
      <Panel position="top-right" style={{ marginTop: DOCK_TOP_RESERVE }}>
        {card}
      </Panel>
    );
  }

  return (
    <NodeToolbar
      nodeId={planNodeId}
      isVisible
      position={placement.side === "left" ? Position.Left : Position.Right}
      align={placement.align}
      offset={16}
    >
      {card}
    </NodeToolbar>
  );
}
