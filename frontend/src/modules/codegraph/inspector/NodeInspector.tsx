import { X } from "lucide-react";
import { NodeToolbar, Position } from "@xyflow/react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { NodeSnippetPanel } from "../NodeSnippetPanel";
import { DataFlowTab } from "./DataFlowTab";
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

/**
 * One node's inspector: source on the Detail tab, the field trace on Data
 * flow. Anchored to the node via NodeToolbar so it tracks pan and zoom for
 * free. The Comments tab arrives with the plan work.
 *
 * Tabs use the shared component with the `line` variant, not `default`: this
 * surface is `bg-surface-raised`, and `--muted` — what the default variant
 * fills its track with — resolves to that same colour, so the segmented track
 * would be invisible here. `line` needs no track.
 */
export function NodeInspector(props: NodeInspectorProps) {
  const { nodeId, snippet, snippetLoading, snippetError, onEdit, onJump, onClose } = props;

  return (
    <NodeToolbar nodeId={nodeId} isVisible position={Position.Right} offset={16}>
      <div className="flex h-[420px] w-105 flex-col overflow-hidden rounded-xl border border-border-subtle bg-surface-raised shadow-[0_12px_32px_rgba(0,0,0,0.45)]">
        <Tabs defaultValue="detail" className="flex min-h-0 flex-1 flex-col gap-0">
          <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-2">
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
    </NodeToolbar>
  );
}
