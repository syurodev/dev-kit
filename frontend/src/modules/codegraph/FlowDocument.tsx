import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RotateCcw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSplitPanelActions } from "@/components/ui/split-panel";
import { tabKey } from "@/stores/workbenchTabs";
import { cn } from "@/lib/utils";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { CodeGraphNode } from "@/bridge/codegraph";
import { FlowCanvas, describeCounts, type CanvasCounts } from "./graph/FlowCanvas";
import { verbChipClass } from "./graph/tokens";
import { shortLabel } from "./graph/labels";
import { NodeInspector } from "./inspector/NodeInspector";
import { useCodeGraphStore } from "./codegraphStore";
import { closeInspector, inspectNode, loadFlow, loadMoreNeighbors, setFlowMode } from "./flowActions";
import { cardData, graphView } from "./graph/graphView";
import { OutlineView } from "./outline/OutlineView";
import { openFile } from "./fileActions";
import { NavButtons } from "./file/NavButtons";
import { describeParams, FLOW_VIEW, MODULE } from "./documents";
import { useDocumentOwnership, useIsFrontDocument } from "./useDocument";
import { flowKey } from "./workspace/identity";
import { Chip } from "@/components/ui/chip";

/**
 * One flow as a tab: the call graph behind an entry point, or behind a class or
 * method picked with @. Its inspector is a context pane of this tab — it
 * shows while this is the document in front and comes back when it is again.
 *
 * Two modes of the same flow, switched in the header: the Outline reads it
 * top to bottom in source order, the Graph draws it as cards. They share the
 * selection, so "Show in Graph" lands on the row's card.
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

  // Both modes share the selection (the inspector's node) and read the same
  // steps: the Outline as rows, the Graph as labels and hidden helpers.
  const selectedNodeId = useCodeGraphStore((s) => (key && s.inspector?.flow === key ? s.inspector.nodeId : null));
  const mode = flow?.mode ?? "outline";
  // The arriving mode fades up only after a switch, not when the tab opens.
  const [switched, setSwitched] = useState(false);
  // The two modes draw their own header, so the tab strip is a new one after
  // a switch: a switch made from the keyboard puts focus back on its tab.
  const page = useRef<HTMLDivElement>(null);
  const refocusTab = useRef(false);
  useEffect(() => {
    if (!refocusTab.current) return;
    refocusTab.current = false;
    const active = document.activeElement;
    if (active && active !== document.body && active.isConnected) return;
    page.current?.querySelector<HTMLElement>('[role="tab"][data-state="active"]')?.focus();
  }, [mode]);
  const view = useMemo(
    () => (flow?.subgraph ? graphView(root, flow.subgraph.nodes, flow.subgraph.edges, flow.steps) : null),
    [flow?.subgraph, flow?.steps, root],
  );
  const buildData = useCallback(
    (n: CodeGraphNode) => cardData(n, view?.hiddenCalls.get(n.id) ?? 0, view?.effects.get(n.id) ?? []),
    [view],
  );
  const showInGraph = useCallback(
    (nodeId: string) => {
      setSwitched(true);
      setFlowMode(folderId, root, "graph");
      void inspectNode(folderId, root, nodeId);
    },
    [folderId, root],
  );
  const workspaceId = ownership.owner?.workspaceId ?? "";
  const openAt = useCallback(
    (nodeId: string, line: number) => {
      const n = flow?.subgraph?.nodes.find((x) => x.id === nodeId);
      if (n?.filePath) void openFile({ workspaceId, folderId, path: n.filePath }, line, { preview: false, beside: true });
    },
    [flow?.subgraph, folderId, workspaceId],
  );

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
  const rootNode = subgraph?.nodes.find((n) => n.id === root);
  const header = (
    <>
      <NavButtons />
      {entry ? (
        <>
          <span className={cn("rounded-sm px-2 py-0.75 text-code-12-medium uppercase", verbChipClass(entry.verb))}>
            {entry.verb}
          </span>
          <span className="truncate text-title-16 text-text-primary" title={entry.target}>{entry.routeId || entry.target}</span>
          {entry.routeId || entry.consumerGroup ? <span className="truncate text-code-11 text-text-tertiary" title={entry.consumerGroup ? `Consumer group: ${entry.consumerGroup}` : undefined}>{entry.routeId ? entry.target : ""}{entry.consumerGroup ? ` · group ${entry.consumerGroup}` : ""}</span> : null}
        </>
      ) : (
        <span className="truncate text-title-16 text-text-primary" title={rootNode?.label ?? root}>
          {rootNode ? shortLabel(rootNode.label, rootNode.kind) : root}
        </span>
      )}
      {several ? <Chip className="max-w-40 truncate" >{ownership.label}</Chip> : null}
    </>
  );
  const modeSwitch = (
    <Tabs
      value={mode}
      onValueChange={(value) => {
        // Arrows and Enter/Space switch with focus on a tab; a click does not need it back.
        refocusTab.current = !!document.activeElement?.closest('[role="tablist"]');
        setSwitched(true);
        setFlowMode(folderId, root, value as "outline" | "graph");
      }}
    >
      <TabsList>
        <TabsTrigger value="outline">Outline</TabsTrigger>
        <TabsTrigger value="graph">Graph</TabsTrigger>
      </TabsList>
    </Tabs>
  );

  if (flow && subgraph && view) {
    // A capped subgraph says so in both modes: the outline's calls into the
    // nodes not loaded yet read as plain calls until they are.
    const partial = subgraph.truncated && (
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
    );
    if (mode === "outline") {
      return (
        <div ref={page} className="flex h-full w-full flex-col overflow-hidden px-4 pt-3">
          <div className="flex shrink-0 items-center justify-between gap-4 pb-1">
            <div className="flex min-w-0 items-center gap-2.5">{header}</div>
            <div className="flex shrink-0 items-center gap-3.5">
              {flow.stepsLoading && <span className="text-code-11 text-text-tertiary">reading…</span>}
              {partial}
              {modeSwitch}
            </div>
          </div>
          <div className="h-px shrink-0 bg-border-subtle" />
          {/* After a switch the body fades up; the header stays where it was. */}
          <div className={cn("flex min-h-0 flex-1 flex-col", switched && "flow-mode-enter")}>
            <OutlineView
              folderId={folderId}
              root={root}
              flow={flow}
              nodes={subgraph.nodes}
              selectedNodeId={selectedNodeId}
              onInspect={onSelect}
              onShowInGraph={showInGraph}
              onOpenAt={openAt}
            />
          </div>
        </div>
      );
    }
    return (
      <div ref={page} className="flex h-full w-full flex-col overflow-hidden">
        <FlowCanvas
          viewportClassName={switched ? "flow-mode-enter" : undefined}
          key={root}
          rootId={root}
          nodes={view.nodes}
          edges={view.edges}
          onSelect={onSelect}
          buildData={buildData}
          edgeLabel={view.edgeLabel}
          edgeTitle={view.edgeTitle}
          selectedNodeId={selectedNodeId}
          focusNodeId={selectedNodeId}
          header={header}
          meta={(counts: CanvasCounts) => (
            <>
              <span className="text-code-11 text-text-tertiary">{describeCounts(counts)}</span>
              {partial}
              {modeSwitch}
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
  const { openPanel, closePanel } = useSplitPanelActions();

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
      // Another flow is another tree (its tab and picks start over); another node of this flow is not.
      { width: 520, onClose: closeInspector, key: `flow:${key}` },
    );
    return () => closePanel();
  }, [node, root, key, folderId, workspaceId, subgraph, snippet, snippetLoading, snippetError, openPanel, closePanel]);
}
