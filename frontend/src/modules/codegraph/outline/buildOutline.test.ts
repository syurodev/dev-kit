import { describe, expect, it } from "bun:test";
import { buildOutline, type OutlineRow } from "./buildOutline";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";

const node = (id: string, label = `acme.P.${id}`): CodeGraphNode => ({
  id, label, kind: "method", layer: "", filePath: "P.java", line: 1, endLine: 9, summary: "",
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 1, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "side effect", ...over,
});
const nodes = (...ids: string[]) => new Map(ids.map((id) => [id, node(id)]));
/** Rows as "depth kind text", the way a reader sees them. */
const show = (rows: OutlineRow[]) =>
  rows.map((r) => {
    const label =
      r.kind === "method" ? r.title
      : r.kind === "frame" ? r.text + (r.exits ? " ↩" : "")
      : r.kind === "call" ? r.title + (r.ref ? ` [${r.ref.reason}]` : "")
      : r.kind === "fold" ? `+${r.count} · ${r.reasons}`
      : "more";
    return `${r.depth} ${r.kind} ${label}`;
  });

describe("buildOutline", () => {
  it("groups steps under their frames and folds hidden runs inside one group", () => {
    const steps: CodeGraphSteps = {
      legacy: false,
      truncated: [],
      steps: {
        process: [
          step({ text: "decide(exchange)", to: "decide", frames: [{ id: "2:5", kind: "try", text: "try" }] }),
          step({ text: "log.error(e)", visible: false, reason: "logging", frames: [{ id: "4:7", kind: "catch", text: "catch E", exits: true }] }),
          step({ text: "quarantine(x)", to: "quarantine", frames: [{ id: "4:7", kind: "catch", text: "catch E", exits: true }] }),
          step({ text: "log.debug(a)", visible: false, reason: "logging", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
          step({ text: "counter(a)", visible: false, reason: "helper", to: "counter", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
          step({ text: "Counter.increment()", visible: false, reason: "metrics", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
        ],
        decide: [],
        quarantine: [],
        counter: [],
      },
    };
    const rows = buildOutline({ rootId: "process", nodes: nodes("process", "decide", "quarantine", "counter"), steps, toggled: {} });
    expect(show(rows)).toEqual([
      "0 method P.process",
      "1 frame try",
      "2 call decide(exchange)",
      "1 frame catch E ↩",
      "2 fold +1 · logging",
      "2 call quarantine(x)",
      "1 frame if Skip ↩",
      "2 fold +3 · logging, helper, metrics",
    ]);
  });

  it("opens a fold in place", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "log.a()", visible: false, reason: "logging" }), step({ text: "send()" })] },
    };
    const closed = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} });
    const fold = closed.find((r) => r.kind === "fold")!;
    const open = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: { [fold.key]: true } });
    expect(show(open)).toEqual(["0 method P.a", "1 fold +1 · logging", "2 call log.a()", "1 call send()"]);
    expect(open.find((r) => r.kind === "call" && r.title === "log.a()")).toMatchObject({ hidden: true });
  });

  it("draws a closure's steps under the call that runs it, not under a heading", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [
          step({ line: 11, col: 9, text: "breaker.executeCallable(() -> …)" }),
          step({ line: 12, col: 13, text: "deliver(p)", to: "deliver", frames: [{ id: "11:9", kind: "closure", text: "breaker.executeCallable(() -> …)" }] }),
        ],
        deliver: [],
      },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "deliver"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call breaker.executeCallable(() -> …)",
      "2 call deliver(p)",
    ]);
  });

  it("lists implementations under an abstract call and expands each", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "decide(x)", to: "decide", impls: ["implA", "implB"] })], decide: [], implA: [step({ text: "acl.find()" })], implB: [] },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "decide", "implA", "implB"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call decide(x)",
      "2 method P.implA",
      "3 call acl.find()",
      "2 method P.implB",
    ]);
  });

  it("draws a method once and points back to it, and marks recursion", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [step({ text: "q(1)", to: "q" }), step({ text: "q(2)", to: "q" }), step({ text: "a()", to: "a" })],
        q: [step({ text: "send()" })],
      },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "q"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call q(1)",
      "2 call send()",
      "1 call q(2) [shown-above]",
      "1 call a() [recursion]",
    ]);
  });

  it("opens three levels of project calls by default", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "b()", to: "b" })], b: [step({ text: "c()", to: "c" })], c: [step({ text: "d()", to: "d" })], d: [step({ text: "e()" })] },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "b", "c", "d"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 call b()", "2 call c()", "3 call d()"]);
    expect(rows[rows.length - 1]).toMatchObject({ kind: "call", expandable: true, open: false });
  });

  it("says where a method's steps were cut", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: ["a"], steps: { a: [step({ text: "x()" })] } };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} }))).toEqual(["0 method P.a", "1 call x()", "1 more more"]);
  });

  it("names a step with no text by its target", () => {
    const steps: CodeGraphSteps = { legacy: true, truncated: [], steps: { a: [step({ text: "", to: "b" })], b: [] } };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "b"), steps, toggled: {} }))).toEqual(["0 method P.a", "1 call P.b"]);
  });

  it("draws a call into a method with no body as a project call that does not expand", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [
          step({ text: "repo.findById(id)", calleeKey: "Repo#findById", target: "findById", reason: "DB read" }),
          step({ text: "", calleeKey: "Repo#save", target: "save", reason: "project" }),
        ],
      },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "findById", "save"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 call repo.findById(id)", "1 call P.save"]);
    expect(rows[1]).toMatchObject({ kind: "call", targetId: "findById", expandable: false, open: false });
    expect(rows[2]).toMatchObject({ kind: "call", targetId: "save", expandable: false });
  });
});
