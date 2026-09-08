import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ICON_BADGE, LAYERS, layerOf } from "../graph/tokens";
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
 * Figma `CodeGraph / Node Panel Content` (482:8289): a 20px-inset header with
 * the 30px icon ring, the short name in Title/13 over "SERVICE · METHOD" in
 * caps, then the Detail / Data flow strip and the body.
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
      <div className="flex shrink-0 items-center gap-2 px-5 pt-4 pb-3 pr-12">
        <span className={ICON_BADGE}>
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.75">
          <span title={node.label} className="truncate text-title-13 text-text-primary">
            {node.label.split(".").slice(-2).join(".")}
          </span>
          <span className="truncate text-label-11-caps text-text-tertiary">
            {visual.label} · {node.kind}
          </span>
        </span>
      </div>

      <div className="shrink-0 px-5 pt-0.5">
        <TabsList className="w-full">
          <TabsTrigger value="detail" className="flex-1">Detail</TabsTrigger>
          <TabsTrigger value="dataflow" className="flex-1">Data flow</TabsTrigger>
        </TabsList>
      </div>

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
