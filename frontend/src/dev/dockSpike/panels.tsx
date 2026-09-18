import { useEffect, useRef, useState } from "react";
import type { IDockviewPanelProps } from "dockview-react";
import { Background, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import type { PaneParams } from "./layoutRules";
import { countMount, nextInstance, updateProbes } from "./probes";

type Props = IDockviewPanelProps<PaneParams>;

function useMountProbe(id: string) {
  const [instance] = useState(nextInstance);
  useEffect(() => countMount(id, instance), [id, instance]);
}

// ── Document: Code Graph canvas ────────────────────────────────────────────
const NODES: Node[] = [
  { id: "c", position: { x: 0, y: 0 }, data: { label: "RefundController.create" } },
  { id: "s", position: { x: 0, y: 120 }, data: { label: "RefundService.process" } },
  { id: "r", position: { x: -110, y: 240 }, data: { label: "RefundRepository.save" } },
  { id: "m", position: { x: 130, y: 240 }, data: { label: "RefundMapper.toDto" } },
  { id: "n", position: { x: 0, y: 360 }, data: { label: "NotificationService.send" } },
];
const EDGES: Edge[] = [
  { id: "cs", source: "c", target: "s" },
  { id: "sr", source: "s", target: "r" },
  { id: "sm", source: "s", target: "m", animated: true },
  { id: "rn", source: "r", target: "n" },
];

function FitOnResize({ id }: { id: string }) {
  const { fitView } = useReactFlow();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current?.parentElement;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => {
      const { width: w, height: h } = entry.contentRect;
      updateProbes((s) => ({ canvasSize: { ...s.canvasSize, [id]: { w: Math.round(w), h: Math.round(h) } } }));
      // React Flow measures a hidden pane as 0; refit once it has a size again.
      if (w > 0 && h > 0) void fitView({ padding: 0.2, duration: 0 });
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [fitView, id]);
  return <div ref={ref} className="hidden" />;
}

export function CanvasPanel({ api }: Props) {
  useMountProbe(api.id);
  return (
    <div className="h-full w-full bg-bg-canvas">
      <ReactFlowProvider>
        <ReactFlow defaultNodes={NODES} defaultEdges={EDGES} fitView proOptions={{ hideAttribution: true }} colorMode="dark">
          <Background gap={16} size={1} />
          <FitOnResize id={api.id} />
        </ReactFlow>
      </ReactFlowProvider>
    </div>
  );
}

// ── Document: note editor ───────────────────────────────────────────────────
// Kept outside the component: layout.mdx makes the store, not the DOM, the
// home of a document's state, so a re-mount must not lose an edit.
const noteText = new Map<string, string>();

export function EditorPanel({ api, params }: Props) {
  useMountProbe(api.id);
  const [value, setValue] = useState(() => noteText.get(api.id) ?? `# ${params.title}\n\nRoadmap review for the platform team.\n`);
  return (
    <div className="h-full overflow-auto p-4">
      <CodeEditor
        value={value}
        onChange={(v) => {
          noteText.set(api.id, v);
          setValue(v);
          // keepAlive: "whenDirty" — an unsaved buffer is never unmounted.
          api.setRenderer("always");
        }}
      />
    </div>
  );
}

// ── Companion: terminal ─────────────────────────────────────────────────────
// A registry, like TerminalWorkspace: the xterm instance outlives any panel
// mount, and a (re-)mount only re-attaches its element.
const terminals = new Map<string, { term: Terminal; fit: FitAddon; el: HTMLDivElement }>();

function terminalFor(id: string) {
  let t = terminals.get(id);
  if (!t) {
    const el = document.createElement("div");
    el.style.height = "100%";
    const term = new Terminal({ fontSize: 12, fontFamily: "Geist Mono Variable, monospace", theme: { background: "#00000000" }, allowTransparency: true });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    let line = "";
    term.write("\x1b[32m➜\x1b[0m  acme-refunds git:(main) ");
    term.onData((d) => {
      if (d === "\r") {
        term.write(`\r\n${line ? `echo: ${line}\r\n` : ""}\x1b[32m➜\x1b[0m  acme-refunds git:(main) `);
        line = "";
      } else if (d === "\x7f") {
        if (line) {
          line = line.slice(0, -1);
          term.write("\b \b");
        }
      } else {
        line += d;
        term.write(d);
      }
    });
    t = { term, fit, el };
    terminals.set(id, t);
    updateProbes((s) => ({ terminalsCreated: s.terminalsCreated + 1 }));
  }
  return t;
}

export function TerminalPanel({ api }: Props) {
  useMountProbe(api.id);
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const t = terminalFor(api.id);
    host.current?.appendChild(t.el);
    const ro = new ResizeObserver(() => {
      try {
        t.fit.fit();
      } catch {
        // hidden: nothing to fit
      }
    });
    if (host.current) ro.observe(host.current);
    return () => ro.disconnect();
  }, [api.id]);
  return <div ref={host} className="h-full w-full px-2 pt-2" />;
}

// ── Companion: agent ────────────────────────────────────────────────────────
export function AgentPanel({ api }: Props) {
  useMountProbe(api.id);
  const [draft, setDraft] = useState("");
  const mountedAt = useRef(new Date().toLocaleTimeString());
  return (
    <div className="flex h-full flex-col gap-3 p-3 text-body-13">
      <div className="text-caption-11 text-text-tertiary">mounted at {mountedAt.current} — a move must not change this</div>
      <div className="flex-1 space-y-3 overflow-auto">
        <p className="text-text-secondary"><b className="text-text-primary">You</b> · Use an optimistic lock on the refund row.</p>
        <p className="text-text-secondary"><b className="text-text-primary">Agent</b> · Added a version column; process() now fails fast instead of double-crediting.</p>
      </div>
      <textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        placeholder="Type here, then drag this pane — the draft must survive"
        className="h-20 resize-none rounded-md border border-border-subtle bg-surface-raised p-2 text-body-13 text-text-primary outline-none"
      />
    </div>
  );
}

// ── Context panes ───────────────────────────────────────────────────────────
export function CodePanel({ api }: Props) {
  useMountProbe(api.id);
  return (
    <pre className="h-full overflow-auto p-3 text-code-12 leading-5 text-text-secondary">{`public Refund process(RefundCommand cmd) {
    var refund = repository.save(cmd.toRefund());
    auditService.log(REFUND_CREATED, refund.id());
    if (refund.status() == Status.PENDING) {
        notificationService.send(refund, REFUND);
    }
    return refund;
}`}</pre>
  );
}

export function DataFlowPanel({ api }: Props) {
  useMountProbe(api.id);
  const rows = [["READ", "cmd.amount"], ["WRITE", "refund.amount"], ["READ", "refund.status"], ["CALL", "notificationService.send"]];
  return (
    <ul className="h-full overflow-auto p-3 text-code-12">
      {rows.map(([k, f]) => (
        <li key={f} className="flex gap-3 py-1">
          <span className="w-12 text-status-info">{k}</span>
          <span className="text-text-primary">{f}</span>
        </li>
      ))}
    </ul>
  );
}

export function MetadataPanel({ api, params }: Props) {
  useMountProbe(api.id);
  return (
    <div className="space-y-2 p-3 text-body-13 text-text-secondary">
      <div className="text-title-13 text-text-primary">{params.title}</div>
      <div>Folder: Work</div>
      <div>Tags: backend, staging</div>
    </div>
  );
}

export const PANEL_COMPONENTS = {
  canvas: CanvasPanel,
  editor: EditorPanel,
  terminal: TerminalPanel,
  agent: AgentPanel,
  code: CodePanel,
  dataflow: DataFlowPanel,
  metadata: MetadataPanel,
};
