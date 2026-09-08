import { useEffect, useState } from "react";
import { X } from "lucide-react";
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

/**
 * A plan node's comment thread, anchored to the node on the canvas.
 *
 * This one stays a popover while Detail and Data flow moved to the right
 * panel, and the split is not arbitrary: a comment is short, written about the
 * node you are pointing at, and done with when you close it. Anchoring is the
 * point. Detail and a call trace are long, scrolling, and outlive the glance.
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
      className="nowheel nopan flex flex-col overflow-hidden rounded-xl border border-border-subtle bg-surface-raised shadow-[0_12px_32px_rgba(0,0,0,0.45)]"
    >
      <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle px-3 py-2">
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="text-title-14 text-text-primary">Comments</span>
          <span title={label} className="truncate text-code-11 text-text-tertiary">
            {label}
          </span>
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="size-6 shrink-0"
          aria-label="Close comments"
          onClick={onClose}
        >
          <X size={14} />
        </Button>
      </div>

      {/* Not decoration: a comment here is handed to the agent with the plan
          and steers what gets built. Letting someone believe it is a private
          note would make the feature actively misleading. */}
      <p className="shrink-0 border-b border-border-subtle px-3 py-1.5 text-caption-11 text-text-tertiary">
        The agent reads this and it steers the implementation — not a private note.
      </p>

      <ScrollArea className="min-h-0 flex-1">
        <ul className="flex flex-col gap-2 p-3">
          {thread?.map((c) => (
            <li
              key={c.commentId}
              className={cn(
                "rounded-lg px-2.5 py-1.5 text-body-12",
                c.author === "agent"
                  ? "bg-surface-tinted text-text-secondary"
                  : "bg-foreground/5 text-text-primary",
              )}
            >
              <span className="mb-0.5 block text-code-11 text-text-tertiary">
                {c.author}
              </span>
              <span className="whitespace-pre-wrap">{c.body}</span>
            </li>
          ))}
          {thread?.length === 0 && (
            <li className="text-body-12 text-muted-foreground">No comments on this node yet.</li>
          )}
          {!thread && <li className="text-body-12 text-muted-foreground">Loading…</li>}
        </ul>
      </ScrollArea>

      <div className="flex shrink-0 flex-col gap-1.5 border-t border-border-subtle p-3">
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          rows={3}
          placeholder="Tell the agent what to do differently here…"
          className="w-full resize-none rounded-md border border-border-subtle bg-surface-tinted px-2.5 py-1.5 text-body-13 text-text-primary placeholder:text-text-tertiary focus-visible:border-ring focus-visible:outline-none"
        />
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            className="h-6 shrink-0 px-2 text-label-12"
            disabled={sending || !draft.trim()}
            onClick={() => void send()}
          >
            {sending ? "Saving…" : "Send"}
          </Button>
          {/* Delivery into a live session is not built. Saying "sent to the
              agent" would be false in every case today — see comments.go. */}
          <span className="text-caption-11 text-text-tertiary">
            Saved. The agent sees this next time it reads the plan.
          </span>
        </div>
        {error && <p className="text-caption-11 text-destructive">{error}</p>}
      </div>
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
