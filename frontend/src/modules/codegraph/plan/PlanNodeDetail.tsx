import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { CodeGraphPlanNode } from "@/bridge/codegraphPlan";
import { PlanNodeGitChanges } from "./PlanNodeGitChanges";
import { PLAN_BADGE, PLAN_STATUS } from "./planStatus";

/**
 * The selected plan node, with its FQN editable.
 *
 * Relinking is the escape hatch for when the agent had a good reason to name a
 * class differently from the plan — it repoints the plan node and changes no
 * code (codegraph.mdx § "Bridge & MCP mới"). Status recomputes from the new FQN
 * on the next read, which is why saving reloads rather than patching in place.
 */
export function PlanNodeDetail({
  node,
  busy,
  onRelink,
}: {
  node: CodeGraphPlanNode;
  busy: boolean;
  onRelink: (fqn: string) => void;
}) {
  const [draft, setDraft] = useState(node.fqn);
  const [editing, setEditing] = useState(false);

  // A different node selected while editing must not carry the old draft over
  // and silently relink the wrong node on save.
  useEffect(() => {
    setDraft(node.fqn);
    setEditing(false);
  }, [node.planNodeId, node.fqn]);

  const status = PLAN_STATUS[node.status];
  const changed = draft.trim() !== node.fqn && draft.trim().length > 0;

  return (
    <div className="flex flex-col gap-1.5 border-b border-border-subtle px-6 py-2">
      <div className="flex items-center gap-2">
        <span className={cn(PLAN_BADGE, status.badge)}>{status.label}</span>
        <span className="truncate text-label-12 text-text-primary">{node.label}</span>
        <span className="ml-auto shrink-0 text-caption-11 text-text-tertiary">{status.hint}</span>
      </div>

      {node.description && (
        <p className="text-caption-11 text-text-secondary">{node.description}</p>
      )}

      <div className="flex items-center gap-1.5">
        {editing ? (
          <>
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              spellCheck={false}
              aria-label="Fully-qualified name"
              className="min-w-0 flex-1 rounded-md border border-border-subtle bg-state-soft px-2 py-1 text-code-11 text-text-primary focus-visible:border-ring focus-visible:outline-none"
            />
            <Button size="xs" disabled={busy || !changed} onClick={() => onRelink(draft.trim())}>
              Relink
            </Button>
            <Button
              size="xs"
              variant="ghost"
              onClick={() => {
                setDraft(node.fqn);
                setEditing(false);
              }}
            >
              Cancel
            </Button>
          </>
        ) : (
          <>
            <span
              title={node.fqn}
              className="min-w-0 flex-1 truncate text-code-11 text-text-tertiary"
            >
              {node.fqn}
            </span>
            <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => setEditing(true)}>
              Relink…
            </Button>
          </>
        )}
      </div>

      <PlanNodeGitChanges realNodeId={node.realNodeId} />
    </div>
  );
}
