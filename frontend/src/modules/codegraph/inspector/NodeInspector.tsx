import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { LAYERS, layerOf } from "../graph/tokens";
import { DetailTab } from "./DetailTab";
import { DataFlowTab } from "./DataFlowTab";
import type { CodeGraphCodeSnippet, CodeGraphNode } from "@/bridge/codegraph";

export type NodeInspectorProps = {
  node: CodeGraphNode;
  snippet: CodeGraphCodeSnippet | null;
  snippetLoading: boolean;
  snippetError: string;
  onEdit?: () => void;
  onJump: (nodeId: string) => void;
};

/**
 * One node's inspector, rendered in the shared right panel.
 *
 * It lived in a canvas popover briefly and every bug that followed came from
 * that: a fixed-size box clipped at the viewport edge, the canvas swallowed the
 * wheel, and code wrapped badly at 400px. Source and a call trace both need
 * width, height and their own scroll, and both outlive a glance at one node —
 * which is a panel, not a popover. Comments stay a popover, on the plan canvas:
 * different object, shorter life, and anchoring is the point there.
 *
 * No close button here — SplitPanel draws its own, hence the header's right
 * padding.
 *
 * Tabs use the `default` variant: this panel is `bg-card`, which resolves to
 * surface/base, while the segmented track is `bg-muted` = surface/raised. Two
 * different colours, so the track reads. On the popover, which WAS
 * surface/raised, it would have been invisible.
 */
export function NodeInspector({
  node,
  snippet,
  snippetLoading,
  snippetError,
  onEdit,
  onJump,
}: NodeInspectorProps) {
  const visual = LAYERS[layerOf(node.layer)];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;

  return (
    <Tabs defaultValue="detail" className="flex min-h-0 flex-1 flex-col gap-0">
      <div className="flex shrink-0 items-center gap-2 px-3 py-2.5 pr-12">
        <span
          className="flex size-7.5 shrink-0 items-center justify-center rounded-control"
          style={{ backgroundColor: `color-mix(in srgb, ${accent} 14%, transparent)` }}
        >
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col">
          <span title={node.label} className="truncate text-title-14 text-text-primary">
            {node.label.split(".").slice(-2).join(".")}
          </span>
          <span className="truncate text-code-11 text-text-tertiary">
            {visual.label} · {node.kind}
          </span>
        </span>
      </div>

      <div className="shrink-0 px-3 pb-2">
        <TabsList className="w-full">
          <TabsTrigger value="detail">Detail</TabsTrigger>
          <TabsTrigger value="dataflow">Data flow</TabsTrigger>
        </TabsList>
      </div>

      <div className="h-px shrink-0 bg-border-subtle" />

      <TabsContent value="detail" className="flex min-h-0 flex-col">
        <DetailTab
          snippet={snippet}
          loading={snippetLoading}
          error={snippetError}
          onEdit={onEdit}
        />
      </TabsContent>

      <TabsContent value="dataflow" className="flex min-h-0 flex-col">
        <DataFlowTab nodeId={node.id} onJump={onJump} />
      </TabsContent>
    </Tabs>
  );
}
