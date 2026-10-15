import { describe, expect, it } from "bun:test";
import { cardData, graphView } from "./graphView";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";

const node = (id: string, over: Partial<CodeGraphNode> = {}): CodeGraphNode => ({
  id, label: `com.acme.Processor.${id}`, kind: "method", layer: "", filePath: "src/Processor.java", line: 7, endLine: 9, summary: "", ...over,
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 1, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "project", ...over,
});

describe("graphView", () => {
  const nodes = ["process", "decide", "impl", "quarantine", "metricPrefix"].map((id) => node(id));
  const edges = [
    { from: "process", to: "decide", kind: "call" },
    { from: "process", to: "quarantine", kind: "call", guard: "!(decision instanceof Skip)" },
    { from: "process", to: "metricPrefix", kind: "call" },
    { from: "decide", to: "impl", kind: "call" },
  ];
  const steps: CodeGraphSteps = {
    legacy: false, truncated: [],
    steps: {
      process: [
        step({ text: "decide(e)", to: "decide", impls: ["impl"], frames: [{ id: "2:5", kind: "try", text: "try" }] }),
        step({ text: "log.x()", visible: false, reason: "logging" }),
        step({ text: "quarantine(e)", to: "quarantine", frames: [{ id: "4:7", kind: "catch", text: "catch AclLookupException", exits: true }] }),
        step({ text: "metricPrefix()", to: "metricPrefix", visible: false, reason: "helper" }),
      ],
      decide: [], impl: [], metricPrefix: [],
      quarantine: [step({ text: "send()", visible: true, reason: "Kafka produce", effect: { kind: "kafka", role: "produce", target: "q" } })],
    },
  };

  it("leaves out helpers, numbers calls and labels them with their innermost frame", () => {
    const view = graphView("process", nodes, edges, steps);
    expect(view.nodes.map((n) => n.id)).toEqual(["process", "decide", "impl", "quarantine"]);
    expect(view.edges.map((e) => `${e.from}->${e.to}:${e.kind}`)).toEqual([
      "process->decide:call", "process->quarantine:call", "decide->impl:implements",
    ]);
    const label = (from: string, to: string) => view.edgeLabel(view.edges.find((e) => e.from === from && e.to === to)!);
    expect(label("process", "decide")).toBe("1 · try");
    expect(label("process", "quarantine")).toBe("2 · catch AclLookupException");
    expect(label("decide", "impl")).toBe("implements");
    expect(view.edgeTitle(view.edges.find((e) => e.to === "quarantine")!)).toBe("!(decision instanceof Skip)");
    expect(view.hiddenCalls.get("process")).toBe(2);
    expect(view.effects.get("quarantine")).toEqual(["Kafka produce"]);
  });

  it("labels the edge into a method with no body by its call's order", () => {
    const repoNodes = [node("process"), node("validate"), node("findById")];
    const repoEdges = [
      { from: "process", to: "validate", kind: "call" },
      { from: "process", to: "findById", kind: "call" },
    ];
    const repoSteps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        process: [
          step({ text: "validate(e)", to: "validate" }),
          step({ text: "repo.findById(id)", target: "findById", reason: "DB read", frames: [{ id: "3:5", kind: "if", text: "if valid" }] }),
        ],
        validate: [],
      },
    };
    const view = graphView("process", repoNodes, repoEdges, repoSteps);
    expect(view.nodes.map((n) => n.id)).toEqual(["process", "validate", "findById"]);
    expect(view.edgeLabel(view.edges.find((e) => e.to === "findById")!)).toBe("2 · if valid");
  });

  it("numbers only the caller's visible project calls, once per callee", () => {
    const g = [node("process"), node("a"), node("b")];
    const e = [{ from: "process", to: "a", kind: "call" }, { from: "process", to: "b", kind: "call" }];
    const st: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        process: [
          step({ text: "send()", reason: "Kafka produce" }),
          step({ text: "a()", to: "a" }),
          step({ text: "a()", to: "a" }),
          step({ text: "b()", to: "b" }),
        ],
        a: [], b: [],
      },
    };
    const view = graphView("process", g, e, st);
    expect(view.edges.map((x) => view.edgeLabel(x))).toEqual(["1", "2"]);
  });

  it("keeps a method one caller hides as a helper and another calls visibly", () => {
    const g = [node("process"), node("decide"), node("fmt")];
    const e = [{ from: "process", to: "fmt", kind: "call" }, { from: "process", to: "decide", kind: "call" }, { from: "decide", to: "fmt", kind: "call" }];
    const st: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        process: [step({ text: "fmt()", to: "fmt", visible: false, reason: "helper" }), step({ text: "decide()", to: "decide" })],
        decide: [step({ text: "fmt()", to: "fmt" })],
        fmt: [],
      },
    };
    expect(graphView("process", g, e, st).nodes.map((n) => n.id)).toEqual(["process", "decide", "fmt"]);
  });

  it("drops what only a hidden helper reached", () => {
    const g = [node("process"), node("decide"), node("fmt"), node("pad")];
    const e = [{ from: "process", to: "decide", kind: "call" }, { from: "process", to: "fmt", kind: "call" }, { from: "fmt", to: "pad", kind: "call" }];
    const st: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        process: [step({ text: "decide()", to: "decide" }), step({ text: "fmt()", to: "fmt", visible: false, reason: "helper" })],
        fmt: [step({ text: "pad()", to: "pad" })],
        decide: [], pad: [],
      },
    };
    const view = graphView("process", g, e, st);
    expect(view.nodes.map((n) => n.id)).toEqual(["process", "decide"]);
    expect(view.edges.map((x) => `${x.from}->${x.to}`)).toEqual(["process->decide"]);
  });

  it("names each effect of a method once", () => {
    const st: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        process: [
          step({ text: "send(a)", reason: "Kafka produce", effect: { kind: "kafka", role: "produce", target: "a" } }),
          step({ text: "send(b)", reason: "Kafka produce", effect: { kind: "kafka", role: "produce", target: "b" } }),
          step({ text: "get()", reason: "HTTP call", effect: { kind: "http", role: "call", target: "GET /x" } }),
          step({ text: "log()", visible: false, reason: "logging", effect: { kind: "db", role: "write", target: "t" } }),
        ],
      },
    };
    expect(graphView("process", [node("process")], [], st).effects.get("process")).toEqual(["Kafka produce", "HTTP call"]);
  });

  it("passes the graph through untouched without steps", () => {
    const view = graphView("process", nodes, edges, null);
    expect(view.nodes).toBe(nodes);
    expect(view.edges).toBe(edges);
    expect(view.edgeLabel(edges[0])).toBeUndefined();
  });
});

describe("cardData", () => {
  it("puts the method first, the class above it and the summary instead of the file", () => {
    const data = cardData(node("quarantine", { summary: "Forwards the record untouched. Then counts it." }), 6, ["Kafka produce"]);
    expect(data).toMatchObject({
      title: "quarantine", caption: "Processor", summary: "Forwards the record untouched.",
      detail: "Processor.java:7", hiddenCalls: 6, effects: ["Kafka produce"],
    });
  });

  it("keeps a class's own name as the title", () => {
    expect(cardData(node("X", { kind: "class", label: "com.acme.Processor" }), 0, [])).toMatchObject({ title: "Processor", caption: "" });
  });
});
