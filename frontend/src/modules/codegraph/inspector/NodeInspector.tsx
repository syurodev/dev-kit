import { X } from "lucide-react";
import {
  NodeToolbar,
  Panel,
  Position,
  useInternalNode,
  useStore,
  useViewport,
} from "@xyflow/react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { NodeSnippetPanel } from "../NodeSnippetPanel";
import { DataFlowTab } from "./DataFlowTab";
import { DOCK_TOP_RESERVE, placeInspector } from "./placeInspector";
import type { CodeGraphCodeSnippet } from "@/bridge/codegraph";

export type NodeInspectorProps = {
  nodeId: string;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;
  onEdit?: () => void;
  onJump: (nodeId: string) => void;
  onClose: () => void;
};

const SIDE = { left: Position.Left, right: Position.Right } as const;

/**
 * One node's inspector: source on the Detail tab, the field trace on Data
 * flow. The Comments tab arrives with the plan work.
 *
 * Placement is measured, not fixed. Anchoring to the node is the good case —
 * it says which node you are reading — but a fixed-size panel beside a node
 * near a canvas edge clips off screen, and in a narrow window there is no side
 * wide enough to anchor to at all. placeInspector decides between the two from
 * the real container and node geometry; see its tests for the rules.
 *
 * Tabs use the shared component with the `line` variant, not `default`: this
 * surface is `bg-surface-raised`, and `--muted` — what the default variant
 * fills its track with — resolves to that same colour, so the segmented track
 * would be invisible here. `line` needs no track.
 */
export function NodeInspector(props: NodeInspectorProps) {
  const { nodeId, snippet, snippetLoading, snippetError, onEdit, onJump, onClose } = props;

  // Container size and viewport transform come from the flow store, so the
  // placement re-runs on pan, zoom and resize without a ResizeObserver here.
  const canvasWidth = useStore((s) => s.width);
  const canvasHeight = useStore((s) => s.height);
  const viewport = useViewport();
  const node = useInternalNode(nodeId);

  // Before the node is measured there is nothing to place against; rendering
  // at a guessed size is what produced the clipped panel in the first place.
  if (!node || !canvasWidth || !canvasHeight) return null;

  const nodeScreenRect = {
    x: node.internals.positionAbsolute.x * viewport.zoom + viewport.x,
    y: node.internals.positionAbsolute.y * viewport.zoom + viewport.y,
    width: (node.measured.width ?? 0) * viewport.zoom,
    height: (node.measured.height ?? 0) * viewport.zoom,
  };
  const placement = placeInspector(nodeScreenRect, {
    width: canvasWidth,
    height: canvasHeight,
  });

  const card = (
    <div
      style={{ width: placement.width, height: placement.height }}
      className="flex flex-col overflow-hidden rounded-xl border border-border-subtle bg-surface-raised shadow-[0_12px_32px_rgba(0,0,0,0.45)]"
    >
      <Tabs defaultValue="detail" className="flex min-h-0 flex-1 flex-col gap-0">
        <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle px-3 py-2">
          <TabsList variant="line" className="flex-1">
            <TabsTrigger value="detail">Detail</TabsTrigger>
            <TabsTrigger value="dataflow">Data flow</TabsTrigger>
          </TabsList>
          <Button
            variant="ghost"
            size="icon"
            className="size-6 shrink-0"
            aria-label="Close inspector"
            onClick={onClose}
          >
            <X size={14} />
          </Button>
        </div>

        <TabsContent value="detail" className="min-h-0 overflow-auto">
          <NodeSnippetPanel
            snippet={snippet}
            loading={snippetLoading}
            error={snippetError}
            onEdit={onEdit}
          />
        </TabsContent>

        <TabsContent value="dataflow" className="flex min-h-0 flex-col">
          <DataFlowTab nodeId={nodeId} onJump={onJump} />
        </TabsContent>
      </Tabs>
    </div>
  );

  if (placement.mode === "docked") {
    // top-right is also CanvasToolbar's corner; DOCK_TOP_RESERVE is the height
    // placeInspector already subtracted, so the two never overlap.
    return (
      <Panel position="top-right" style={{ marginTop: DOCK_TOP_RESERVE }}>
        {card}
      </Panel>
    );
  }

  return (
    <NodeToolbar
      nodeId={nodeId}
      isVisible
      position={SIDE[placement.side]}
      align={placement.align}
      offset={16}
    >
      {card}
    </NodeToolbar>
  );
}
