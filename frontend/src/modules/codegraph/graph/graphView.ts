import type { CodeGraphEdge, CodeGraphNode, CodeGraphSteps } from "@/bridge/codegraph";
import { shortLabel } from "./labels";
import { firstSentence } from "../outline/buildOutline";
import type { ApiNodeData } from "./ApiNode";

/**
 * What the Graph mode draws, read off the same steps as the Outline: helpers
 * the outline folds away are not cards; an edge says which call it is (its
 * order among the caller's visible steps) and under which structure; a call
 * into an override is drawn as "implements". Without steps the graph is
 * today's, untouched.
 */
export type GraphView = {
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  hiddenCalls: ReadonlyMap<string, number>;
  effects: ReadonlyMap<string, string[]>;
  edgeLabel: (edge: CodeGraphEdge) => string | undefined;
  edgeTitle: (edge: CodeGraphEdge) => string | undefined;
};

const FRAME_LABEL_MAX = 40;
const EFFECT_NAMES: Record<string, string> = { kafka: "Kafka", http: "HTTP", redis: "Redis", db: "DB" };

const clipLabel = (text: string) => (text.length <= FRAME_LABEL_MAX ? text : `${text.slice(0, FRAME_LABEL_MAX - 1)}…`);

export function graphView(rootId: string, nodes: CodeGraphNode[], edges: CodeGraphEdge[], steps: CodeGraphSteps | null): GraphView {
  if (!steps) {
    return { nodes, edges, hiddenCalls: new Map(), effects: new Map(), edgeLabel: () => undefined, edgeTitle: (e) => e.guard || undefined };
  }
  const incoming = new Map<string, { visible: boolean; reason: string }[]>();
  const implements_ = new Set<string>();
  const hiddenCalls = new Map<string, number>();
  const effects = new Map<string, string[]>();
  const firstCall = new Map<string, { order: number; frame: string }>();

  for (const [from, list] of Object.entries(steps.steps)) {
    let order = 0;
    let hidden = 0;
    const seen = new Set<string>();
    for (const s of list) {
      if (s.visible) order++;
      else hidden++;
      // A call into a method with no body names its node in `target`; the
      // graph still has an edge to it.
      const callee = s.to ?? s.target;
      if (callee) {
        const arr = incoming.get(callee) ?? [];
        arr.push({ visible: s.visible, reason: s.reason });
        incoming.set(callee, arr);
        if (s.visible && !firstCall.has(`${from}->${callee}`)) {
          firstCall.set(`${from}->${callee}`, { order, frame: s.frames[s.frames.length - 1]?.text ?? "" });
        }
      }
      if (s.to) for (const impl of s.impls ?? []) implements_.add(`${s.to}->${impl}`);
      if (s.visible && s.effect) {
        const name = `${EFFECT_NAMES[s.effect.kind] ?? s.effect.kind} ${s.effect.role}`;
        if (!seen.has(name)) {
          seen.add(name);
          effects.set(from, [...(effects.get(from) ?? []), name]);
        }
      }
    }
    if (hidden > 0) hiddenCalls.set(from, hidden);
  }

  const helper = new Set<string>();
  for (const [id, calls] of incoming) {
    if (id !== rootId && calls.length > 0 && calls.every((c) => !c.visible && c.reason === "helper")) helper.add(id);
  }
  const keptNodes = nodes.filter((n) => !helper.has(n.id));
  const keptEdges = edges
    .filter((e) => !helper.has(e.from) && !helper.has(e.to))
    .map((e) => (implements_.has(`${e.from}->${e.to}`) ? { ...e, kind: "implements" } : e));

  return {
    nodes: keptNodes,
    edges: keptEdges,
    hiddenCalls,
    effects,
    edgeLabel: (e) => {
      if (e.kind === "implements") return "implements";
      const call = firstCall.get(`${e.from}->${e.to}`);
      if (!call) return undefined;
      return call.frame ? `${call.order} · ${clipLabel(call.frame)}` : String(call.order);
    },
    edgeTitle: (e) => e.guard || undefined,
  };
}

/** A card's data: the method as the title, its class above it, the doc comment's first sentence. */
export function cardData(n: CodeGraphNode, hiddenCalls: number, effects: string[]): ApiNodeData & { ariaLabel: string } {
  const short = shortLabel(n.label, n.kind);
  const dot = n.kind === "method" ? short.indexOf(".") : -1;
  const file = n.filePath.split("/").pop() ?? n.filePath;
  return {
    layer: n.layer,
    title: dot >= 0 ? short.slice(dot + 1) : short,
    caption: dot >= 0 ? short.slice(0, dot) : "",
    titleTooltip: n.label,
    detail: `${file}:${n.line}`,
    detailTooltip: `${n.filePath}:${n.line}`,
    summary: firstSentence(n.summary),
    hiddenCalls,
    effects,
    ariaLabel: `${n.layer || n.kind} ${n.label}`,
  };
}
