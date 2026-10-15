import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";
import type { FlowDoc } from "../codegraphStore";
import { OutlineView } from "./OutlineView";

const node = (id: string, summary = ""): CodeGraphNode => ({
  id, label: `acme.P.${id}`, kind: "method", layer: "", filePath: "P.java", line: 3, endLine: 9, summary,
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 5, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "side effect", ...over,
});

const flow = (steps: CodeGraphSteps | null, over: Partial<FlowDoc> = {}): FlowDoc => ({
  folderId: "f", subgraph: null, pages: 1, loading: false, loadingMore: false, error: "",
  steps, stepsLoading: false, stepsError: "", mode: "outline", toggled: {}, ...over,
});

function render(doc: FlowDoc, nodes: CodeGraphNode[], selectedNodeId: string | null = null) {
  return renderToStaticMarkup(
    <OutlineView
      folderId="f"
      root="a"
      flow={doc}
      nodes={nodes}
      selectedNodeId={selectedNodeId}
      onInspect={() => {}}
      onShowInGraph={() => {}}
      onOpenAt={() => {}}
    />,
  );
}

describe("OutlineView", () => {
  test("draws one row of each kind", () => {
    const ifOk = { id: "2:5", kind: "if", text: "if (ok)", exits: true };
    const steps: CodeGraphSteps = {
      legacy: false,
      truncated: ["a"],
      steps: {
        a: [
          step({ text: "handle(x)", calleeKey: "acme.P#handle", to: "handle", impls: ["special"], frames: [ifOk] }),
          step({ text: "log.info(x)", calleeKey: "org.slf4j.Logger#info", visible: false, reason: "logging", frames: [ifOk] }),
          step({
            text: "repo.findById(id)", calleeKey: "acme.Repo#findById", target: "findById", reason: "DB read",
            effect: { kind: "db", role: "read", target: "orders" },
          }),
          step({ text: "builder.send()", calleeKey: "lib.Client#send", reason: "side effect" }),
        ],
        handle: [step({ text: "a(y)", calleeKey: "acme.P#a", to: "a" })],
        special: [],
      },
    };
    const html = render(flow(steps), [node("a", "Runs the flow. Then more."), node("handle"), node("special"), node("findById")], "handle");

    // Method rows: the root with its summary's first sentence, the overridden
    // method's own body marked as the base, and an override under it.
    expect(html).toContain("P.a");
    expect(html).toContain("— Runs the flow.");
    expect(html).not.toContain("Then more.");
    expect(html).toContain("(base)");
    expect(html).toContain("P.special");
    expect(html).toContain("↳ 1 implementation<");
    // A frame, which leaves the method.
    expect(html).toContain("if (ok)");
    expect(html).toContain('aria-label="leaves"');
    // A fold of what the outline hides, with why.
    expect(html).toContain("+1 call · logging");
    // A call into a method with no body is a project call (bold) with its effect.
    expect(html).toMatch(/font-semibold text-text-primary"[^>]*>repo\.findById\(id\)/);
    expect(html).toContain("DB read");
    // A library call stays plain.
    expect(html).toMatch(/text-text-secondary"[^>]*>builder\.send\(\)/);
    // A call back into the root reads as recursion; the method is cut short.
    expect(html).toContain("↻ recursion");
    expect(html).toContain("… more steps not shown");
    // The selection from the graph marks its rows.
    expect(html).toContain('aria-selected="true"');
  });

  test("rows are a tree a screen reader can walk", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: [], steps: { a: [step({ text: "send()" }), step({ text: "close()" })] } };
    const html = render(flow(steps), [node("a")]);
    expect(html).toMatch(/role="tree"[^>]*aria-activedescendant="[^"]+-row-0"/);
    expect(html).toContain('aria-level="2" aria-posinset="2" aria-setsize="2"');
  });

  test("a failed re-read keeps the rows on screen, under a banner", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: [], steps: { a: [step({ text: "send()" })] } };
    const html = render(flow(steps, { stepsError: "The index is busy." }), [node("a")]);
    expect(html).toContain("Could not re-read the flow");
    expect(html).toContain("The index is busy.");
    expect(html).toContain("Retry");
    expect(html).toContain("send()");
  });

  test("an old index says how to see branches", () => {
    const steps: CodeGraphSteps = { legacy: true, truncated: [], steps: { a: [] } };
    expect(render(flow(steps), [node("a")])).toContain("Re-index to see them");
  });

  test("waiting and failing", () => {
    expect(render(flow(null), [node("a")])).toContain("Reading the flow…");
    const failed = render(flow(null, { stepsError: "The index is gone." }), [node("a")]);
    expect(failed).toContain("The index is gone.");
    expect(failed).toContain("Try again");
  });
});
