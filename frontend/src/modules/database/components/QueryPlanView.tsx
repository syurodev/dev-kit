import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { copyText } from "../../../bridge/clipboard";
import {
  Sliders,
  AlertTriangle,
  X,
  Clock,
  Layers,
  Copy,
  Check,
  ChevronRight,
  ChevronDown,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PlanNode, QueryPlan } from "../../../bridge/database";
import { cn } from "@/lib/utils";
import { planGraph } from "../plan/planGraph";
import { PlanDiagram } from "../plan/PlanDiagram";

export function QueryPlanView({
  plan,
  onClose,
}: {
  plan: QueryPlan;
  onClose: () => void;
}) {
  const [activeTab, setActiveTab] = useState<"diagram" | "visual" | "raw">("diagram");
  const [copied, setCopied] = useState(false);

  /*
   * The body's height, animated between tabs.
   *
   * Each tab is as tall as it wants (the diagram a fixed 60vh, the others
   * their content up to the same cap), and switching used to snap the whole
   * modal to the new size in one frame. The inner box keeps its natural
   * height; the outer one moves to it.
   */
  const motionOff = useReducedMotion() ?? false;
  const bodyRef = useRef<HTMLDivElement>(null);
  const [bodyHeight, setBodyHeight] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const sync = () => setBodyHeight(el.offsetHeight);
    sync();
    const observer = new ResizeObserver(sync);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Flatten nodes for calculating max self time and statistics
  const flattenedNodes: PlanNode[] = [];
  function collect(node: PlanNode) {
    flattenedNodes.push(node);
    if (node.children) {
      node.children.forEach(collect);
    }
  }
  if (plan.nodes) {
    plan.nodes.forEach(collect);
  }

  // The same tree, as a diagram. Pure and tested in plan/planGraph; this
  // component only decides which of the three views is on screen.
  const graph = useMemo(() => planGraph(plan), [plan]);

  const maxSelfMillis = Math.max(1, ...flattenedNodes.map((n) => n.selfMillis || 0));
  const totalSelfMillis = flattenedNodes.reduce((acc, n) => acc + (n.selfMillis || 0), 0);

  // Identify high-deviation nodes (> 10x ratio)
  const highDeviationNodes = flattenedNodes.filter((n) => {
    if (n.estimatedRows <= 0 || n.actualRows <= 0) return false;
    const ratio = Math.max(n.estimatedRows / n.actualRows, n.actualRows / n.estimatedRows);
    return ratio >= 10;
  });

  function handleCopyRaw() {
    void copyText(plan.raw);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4">
      <div className="bg-surface-base border border-border rounded-xl shadow-2xl w-full max-w-4xl max-h-[90vh] flex flex-col overflow-hidden animate-in fade-in zoom-in-95 duration-(--motion-base)">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-surface-raised/50">
          <div className="flex items-center gap-2.5">
            <Sliders className="w-4 h-4 text-action-primary" />
            <h2 className="text-title-14 text-text-primary">Query Plan Inspector</h2>
            {plan.analyzed ? (
              <Badge variant="outline" className="text-caption-11 text-status-success border-status-success/30 bg-status-success/10">
                EXPLAIN ANALYZE · Executed & Rolled Back
              </Badge>
            ) : (
              <Badge variant="secondary" className="text-caption-11">
                EXPLAIN · Plan Only (No execution)
              </Badge>
            )}
          </div>

          <div className="flex items-center gap-2">
            {/* The app's Tabs, list only: the body below keeps its own
                height animation, which TabsContent's slide would fight. */}
            <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as typeof activeTab)}>
              <TabsList>
                <TabsTrigger value="diagram">Diagram</TabsTrigger>
                <TabsTrigger value="visual">Visual Flame Graph</TabsTrigger>
                <TabsTrigger value="raw">Raw Output</TabsTrigger>
              </TabsList>
            </Tabs>

            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-text-tertiary hover:text-text-primary"
              onClick={onClose}
            >
              <X className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {/* Metric Highlights Bar */}
        <div className="px-4 py-2.5 bg-surface-raised/30 border-b border-border flex flex-wrap items-center justify-between gap-3 text-body-12">
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-1.5 text-text-secondary">
              <Clock className="w-3.5 h-3.5 text-text-tertiary" />
              <span>Total Self Time:</span>
              <span className="font-mono font-semibold text-text-primary">
                {totalSelfMillis.toFixed(2)} ms
              </span>
            </div>

            <div className="flex items-center gap-1.5 text-text-secondary">
              <Layers className="w-3.5 h-3.5 text-text-tertiary" />
              <span>Plan Steps:</span>
              <span className="font-mono font-semibold text-text-primary">
                {flattenedNodes.length}
              </span>
            </div>
          </div>

          {highDeviationNodes.length > 0 && (
            <div className="flex items-center gap-1 text-caption-11-medium text-status-warning bg-amber-500/10 px-2 py-0.5 rounded border border-amber-500/30">
              <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
              <span>
                {highDeviationNodes.length} step(s) with &gt;10× estimate-vs-actual row deviation (stale stats)
              </span>
            </div>
          )}
        </div>

        {/* Content Area. The diagram takes the whole box and scrolls itself
            — a canvas inside an overflow-y-auto would scroll twice and pan
            never. */}
        <motion.div
          className="shrink-0 overflow-hidden bg-surface-base"
          initial={false}
          animate={{ height: bodyHeight ?? "auto" }}
          transition={motionOff ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        >
        <div
          ref={bodyRef}
          className={cn(
            // A concrete height, because React Flow measures its parent and
            // this modal is sized by its content: a canvas inside a box with
            // no height of its own draws nothing. The other tabs share the
            // same cap so the modal never grows past the diagram's size.
            activeTab === "diagram"
              ? "flex h-[60vh] overflow-hidden"
              : "max-h-[60vh] overflow-y-auto p-4",
          )}
        >
          {activeTab === "diagram" ? (
            <PlanDiagram graph={graph} />
          ) : activeTab === "visual" ? (
            <div className="space-y-2">
              {plan.nodes && plan.nodes.length > 0 ? (
                plan.nodes.map((node, i) => (
                  <PlanNodeItem
                    key={i}
                    node={node}
                    maxSelfMillis={maxSelfMillis}
                    totalSelfMillis={totalSelfMillis}
                  />
                ))
              ) : (
                <div className="text-center py-12 text-body-12 text-text-tertiary">
                  No structured plan nodes available. View raw output instead.
                </div>
              )}
            </div>
          ) : (
            <div className="relative">
              <Button
                variant="outline"
                size="sm"
                className="absolute top-2 right-2 h-7 text-label-12 gap-1 z-10"
                onClick={handleCopyRaw}
              >
                {copied ? <Check className="w-3 h-3 text-status-success" /> : <Copy className="w-3 h-3" />}
                <span>{copied ? "Copied" : "Copy Raw"}</span>
              </Button>
              <pre className="p-4 rounded-lg bg-surface-raised border border-border text-code-12 text-text-primary whitespace-pre overflow-x-auto">
                {plan.raw || "No raw plan output"}
              </pre>
            </div>
          )}
        </div>
        </motion.div>

        {/* Footer */}
        <div className="px-4 py-2.5 border-t border-border bg-surface-raised/40 flex items-center justify-between text-body-12 text-text-tertiary">
          <span>Bars indicate individual step self-time. Estimate gaps beyond 10× indicate out-of-date planner statistics.</span>
          <Button variant="secondary" size="sm" onClick={onClose}>
            Done
          </Button>
        </div>
      </div>
    </div>
  );
}

function PlanNodeItem({
  node,
  maxSelfMillis,
  totalSelfMillis,
}: {
  node: PlanNode;
  maxSelfMillis: number;
  totalSelfMillis: number;
}) {
  const [expanded, setExpanded] = useState(true);

  const percentOfMax = maxSelfMillis > 0 ? (node.selfMillis / maxSelfMillis) * 100 : 0;
  const percentOfTotal = totalSelfMillis > 0 ? (node.selfMillis / totalSelfMillis) * 100 : 0;

  let rowRatio = 1;
  let hasHighDeviation = false;
  if (node.estimatedRows > 0 && node.actualRows > 0) {
    rowRatio = Math.max(node.estimatedRows / node.actualRows, node.actualRows / node.estimatedRows);
    hasHighDeviation = rowRatio >= 10;
  }

  const isSeqScan = node.operation.toLowerCase().includes("seq scan");
  const isSlow = percentOfTotal >= 25;

  return (
    <div className="space-y-1" style={{ marginLeft: `${node.depth * 18}px` }}>
      <div
        className={`p-2.5 rounded-lg border text-body-12 flex flex-col gap-1.5 transition-colors ${
          hasHighDeviation
            ? "border-amber-500/40 bg-amber-500/5 hover:bg-amber-500/10"
            : isSlow
            ? "border-status-danger/30 bg-status-danger/5 hover:bg-status-danger/10"
            : "border-border bg-surface-raised hover:bg-surface-raised/80"
        }`}
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          {/* Operation Title */}
          <div className="flex items-center gap-1.5 min-w-0">
            {node.children && node.children.length > 0 && (
              <button
                type="button"
                onClick={() => setExpanded(!expanded)}
                className="text-text-tertiary hover:text-text-primary p-0.5"
              >
                {expanded ? (
                  <ChevronDown className="w-3.5 h-3.5" />
                ) : (
                  <ChevronRight className="w-3.5 h-3.5" />
                )}
              </button>
            )}
            <span className="text-code-12-medium text-text-primary">
              {node.operation}
            </span>

            {isSeqScan && (
              <Badge variant="outline" className="text-caption-11 px-1 py-0 text-amber-500 border-amber-500/30">
                Seq Scan
              </Badge>
            )}

            {hasHighDeviation && (
              <Badge variant="destructive" className="text-caption-11 px-1.5 py-0 bg-amber-500/15 text-amber-500 border-amber-500/30">
                {rowRatio.toFixed(0)}× Deviation
              </Badge>
            )}
          </div>

          {/* Time & Rows Meta */}
          <div className="flex items-center gap-3 text-code-12">
            {node.actualRows > 0 ? (
              <div className="text-text-tertiary text-caption-11">
                <span>Est: {node.estimatedRows.toLocaleString()}</span>
                <span className="mx-1">/</span>
                <span className="text-text-primary font-medium">
                  Act: {node.actualRows.toLocaleString()}
                </span>
              </div>
            ) : node.estimatedRows > 0 ? (
              <div className="text-text-tertiary text-caption-11">
                Est rows: {node.estimatedRows.toLocaleString()}
              </div>
            ) : null}

            <span className="font-semibold text-text-primary">
              {node.selfMillis.toFixed(2)} ms
            </span>
          </div>
        </div>

        {/* Self-time visual bar (Flame graph width) */}
        {node.selfMillis > 0 && (
          <div className="w-full bg-surface-base h-1.5 rounded-full overflow-hidden border border-border/50">
            <div
              className={`h-full rounded-full transition-all ${
                isSlow
                  ? "bg-status-danger"
                  : hasHighDeviation
                  ? "bg-amber-500"
                  : "bg-action-primary"
              }`}
              style={{ width: `${Math.max(2, percentOfMax)}%` }}
            />
          </div>
        )}

        {/* Detail string */}
        {node.detail && (
          <div className="text-code-11 text-text-secondary bg-surface-base/80 p-1.5 rounded border border-border/40 truncate">
            {node.detail}
          </div>
        )}
      </div>

      {/* Children */}
      {expanded && node.children && node.children.length > 0 && (
        <div className="space-y-1 mt-1">
          {node.children.map((child, idx) => (
            <PlanNodeItem
              key={idx}
              node={child}
              maxSelfMillis={maxSelfMillis}
              totalSelfMillis={totalSelfMillis}
            />
          ))}
        </div>
      )}
    </div>
  );
}
