import { useCallback, useEffect, useMemo } from "react";
import { RotateCcw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSplitPanel } from "@/components/ui/split-panel";
import { tabKey } from "@/stores/workbenchTabs";
import { cn } from "@/lib/utils";
import { FlowCanvas, describeCounts, type CanvasCounts } from "./graph/FlowCanvas";
import { methodChipClass } from "./graph/tokens";
import { shortLabel } from "./graph/labels";
import { NodeInspector } from "./inspector/NodeInspector";
import { useCodeGraphStore } from "./codegraphStore";
import { closeInspector, inspectNode, loadFlow, loadMoreNeighbors, openFile } from "./codegraphActions";
import { FLOW_VIEW, MODULE } from "./documents";
import { useIsFrontDocument, useProjectReady } from "./useDocument";

/**
 * One flow as a tab: the call graph behind an endpoint, or behind a class or
 * method picked with @. Its inspector is a context pane of this tab — it
 * shows while this is the document in front and comes back when it is again.
 */
export function FlowDocument({ params }: { params?: Record<string, unknown> }) {
  const root = typeof params?.root === "string" ? params.root : "";
  const endpointId = typeof params?.endpoint === "string" ? params.endpoint : undefined;
  const tabId = tabKey(MODULE, FLOW_VIEW, params);

  const ready = useProjectReady();
  const flow = useCodeGraphStore((s) => s.flows[root]);
  const endpoint = useCodeGraphStore((s) => (endpointId ? s.endpoints.find((ep) => ep.id === endpointId) : undefined));
  const front = useIsFrontDocument(tabId);

  // A tab restored after a relaunch, or one whose flow a re-index dropped,
  // has nothing in the store yet: it asks once the project can answer.
  useEffect(() => {
    if (ready && root && !flow) void loadFlow(root);
  }, [ready, root, flow]);

  const onSelect = useCallback((nodeId: string) => void inspectNode(root, nodeId), [root]);
  useInspectorPane(root, front);

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
            endpoint ? (
              <>
                <span className={cn("rounded-sm px-2 py-0.75 text-code-12-medium uppercase", methodChipClass(endpoint.httpMethod))}>
                  {endpoint.httpMethod}
                </span>
                <span className="truncate text-title-16 text-text-primary">{endpoint.path}</span>
              </>
            ) : (
              <span className="truncate text-title-16 text-text-primary" title={rootNode?.label ?? root}>
                {rootNode ? shortLabel(rootNode.label, rootNode.kind) : root}
              </span>
            )
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
                    onClick={() => void loadMoreNeighbors(root)}
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
        <Button variant="outline" className="bg-surface-raised" onClick={() => void loadFlow(root, true)}>
          Try again
          <RotateCcw className="size-4" />
        </Button>
      </EmptyState>
    );
  }

  return (
    <EmptyState
      busy
      title={endpoint ? `Tracing ${endpoint.httpMethod} ${endpoint.path}` : "Tracing the flow"}
      body={ready ? "Following the calls from the handler through services and repositories." : "Waiting for the project's index."}
    />
  );
}

/**
 * Opens the inspector while this flow is in front and a node of it is
 * picked; closes it when either stops being true. Opening the node's code
 * puts the file beside the flow, so the graph stays on screen.
 */
function useInspectorPane(root: string, front: boolean) {
  const inspector = useCodeGraphStore((s) => s.inspector);
  const subgraph = useCodeGraphStore((s) => s.flows[root]?.subgraph);
  const snippet = useCodeGraphStore((s) => s.snippet);
  const snippetLoading = useCodeGraphStore((s) => s.snippetLoading);
  const snippetError = useCodeGraphStore((s) => s.snippetError);
  const node = useMemo(
    () => (front && inspector?.flow === root ? (subgraph?.nodes.find((n) => n.id === inspector.nodeId) ?? null) : null),
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
        onEdit={snippet ? () => openFile(snippet.filePath, snippet.line, { preview: false, beside: true }) : undefined}
        onJump={(id) => void inspectNode(root, id)}
      />,
      { width: 520, onClose: closeInspector },
    );
    return () => closePanel();
  }, [node, root, snippet, snippetLoading, snippetError, openPanel, closePanel]);
}
