import { useCallback, useEffect, useMemo } from "react";
import { RotateCcw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSplitPanel } from "@/components/ui/split-panel";
import { tabKey } from "@/stores/workbenchTabs";
import { cn } from "@/lib/utils";
import { FlowCanvas, describeCounts, type CanvasCounts } from "./graph/FlowCanvas";
import { verbChipClass } from "./graph/tokens";
import { shortLabel } from "./graph/labels";
import { NodeInspector } from "./inspector/NodeInspector";
import { useCodeGraphStore } from "./codegraphStore";
import { closeInspector, inspectNode, loadFlow, loadMoreNeighbors } from "./flowActions";
import { openFile } from "./fileActions";
import { describeParams, FLOW_VIEW, MODULE } from "./documents";
import { useDocumentOwnership, useIsFrontDocument } from "./useDocument";
import { flowKey } from "./workspace/identity";
import { Chip } from "@/components/ui/chip";

/**
 * One flow as a tab: the call graph behind an entry point, or behind a class or
 * method picked with @. Its inspector is a context pane of this tab — it
 * shows while this is the document in front and comes back when it is again.
 *
 * A flow belongs to one folder (its params name it) and is drawn from that
 * folder's index only: it never mixes in another folder's nodes, and it says
 * whose it is when the workspace has more than one.
 */
export function FlowDocument({ params }: { params?: Record<string, unknown> }) {
  const doc = describeParams(FLOW_VIEW, params);
  const root = doc.root ?? "";
  const entryPointId = doc.entryPoint;
  const tabId = tabKey(MODULE, FLOW_VIEW, params);

  const ownership = useDocumentOwnership(doc);
  const folderId = ownership.owner?.folderId ?? "";
  const key = folderId && root ? flowKey(folderId, root) : "";
  const flow = useCodeGraphStore((s) => (key ? s.flows[key] : undefined));
  const entry = useCodeGraphStore((s) =>
    entryPointId && folderId ? s.data[folderId]?.entries?.entries.find((ep) => ep.id === entryPointId) : undefined,
  );
  const several = useCodeGraphStore((s) => s.folders.length > 1);
  const front = useIsFrontDocument(tabId);
  const ready = ownership.relation === "current";

  // A tab restored after a relaunch, or one whose flow a re-index dropped,
  // has nothing in the store yet: it asks once its folder is known.
  useEffect(() => {
    if (ready && root && !flow) void loadFlow(folderId, root);
  }, [ready, root, flow, folderId]);

  const onSelect = useCallback((nodeId: string) => void inspectNode(folderId, root, nodeId), [folderId, root]);
  useInspectorPane(folderId, root, key, front, ownership.owner?.workspaceId ?? "");

  if (ownership.relation === "removed" || ownership.relation === "other-workspace" || !ownership.owner) {
    return (
      <EmptyState
        icon={TriangleAlert}
        title={ownership.relation === "other-workspace" ? "This flow is from another workspace" : "This flow's folder is not in the workspace"}
        body="Switch to its workspace in the title bar to draw it, or open the entry point again from Entry points."
      />
    );
  }

  const subgraph = flow?.subgraph;
  if (subgraph) {
    const rootNode = subgraph.nodes.find((n) => n.id === root);
    return (
      <div className="flex h-full w-full flex-col overflow-hidden">
        <FlowCanvas
          key={root}
          rootId={root}
          nodes={subgraph.nodes}
          edges={subgraph.edges}
          onSelect={onSelect}
          header={
            <>
              {entry ? (
                <>
                  <span className={cn("rounded-sm px-2 py-0.75 text-code-12-medium uppercase", verbChipClass(entry.verb))}>
                    {entry.verb}
                  </span>
                  <span className="truncate text-title-16 text-text-primary">{entry.target}</span>
                </>
              ) : (
                <span className="truncate text-title-16 text-text-primary" title={rootNode?.label ?? root}>
                  {rootNode ? shortLabel(rootNode.label, rootNode.kind) : root}
                </span>
              )}
              {several ? <Chip className="max-w-40 truncate" >{ownership.label}</Chip> : null}
            </>
          }
          meta={(counts: CanvasCounts) => (
            <>
              <span className="text-code-11 text-text-tertiary">{describeCounts(counts)}</span>
              {subgraph.truncated && (
                <>
                  <span title="The backend capped this subgraph; some nodes are not shown yet." className="text-code-11 text-status-warning">
                    partial
                  </span>
                  <Button
                    variant="outline"
                    size="xs"
                    className="bg-surface-raised"
                    disabled={flow.loadingMore || !subgraph.continuation}
                    onClick={() => void loadMoreNeighbors(folderId, root)}
                  >
                    {flow.loadingMore ? "Loading…" : "Load more"}
                  </Button>
                </>
              )}
            </>
          )}
          hint={
            <>
              <strong className="text-text-primary">Click</strong> to inspect
            </>
          }
        />
      </div>
    );
  }

  if (flow?.error) {
    return (
      <EmptyState icon={TriangleAlert} tone="danger" title="Could not draw this flow" body={flow.error}>
        <Button variant="outline" className="bg-surface-raised" onClick={() => void loadFlow(folderId, root, true)}>
          Try again
          <RotateCcw className="size-4" />
        </Button>
      </EmptyState>
    );
  }

  return (
    <EmptyState
      busy
      title={entry ? `Tracing ${entry.verb} ${entry.target}` : "Tracing the flow"}
      body={ready ? "Following the calls from the handler through services and repositories." : "Waiting for the project's index."}
    />
  );
}

/**
 * Opens the inspector while this flow is in front and a node of it is
 * picked; closes it when either stops being true. Opening the node's code
 * puts the file beside the flow, so the graph stays on screen.
 */
function useInspectorPane(folderId: string, root: string, key: string, front: boolean, workspaceId: string) {
  const inspector = useCodeGraphStore((s) => s.inspector);
  const subgraph = useCodeGraphStore((s) => (key ? s.flows[key]?.subgraph : undefined));
  const snippet = useCodeGraphStore((s) => s.snippet);
  const snippetLoading = useCodeGraphStore((s) => s.snippetLoading);
  const snippetError = useCodeGraphStore((s) => s.snippetError);
  const node = useMemo(
    () => (front && key && inspector?.flow === key ? (subgraph?.nodes.find((n) => n.id === inspector.nodeId) ?? null) : null),
    [front, inspector, root, subgraph],
  );
  const { openPanel, closePanel } = useSplitPanel();

  useEffect(() => {
    if (!node) return;
    openPanel(
      <NodeInspector
        node={node}
        snippet={snippet}
        snippetLoading={snippetLoading}
        snippetError={snippetError}
        // The node's file is in the flow's own folder, whichever folder is
        // the primary or in scope.
        onEdit={snippet ? () => void openFile({ workspaceId, folderId, path: snippet.filePath }, snippet.line, { preview: false, beside: true }) : undefined}
        onJump={(id) => void inspectNode(folderId, root, id)}
        canJump={(id) => Boolean(subgraph?.nodes.some((n) => n.id === id))}
        onOpenFile={(path, line) => void openFile({ workspaceId, folderId, path }, line, { preview: false, beside: true })}
      />,
      { width: 520, onClose: closeInspector },
    );
    return () => closePanel();
  }, [node, root, folderId, workspaceId, subgraph, snippet, snippetLoading, snippetError, openPanel, closePanel]);
}
