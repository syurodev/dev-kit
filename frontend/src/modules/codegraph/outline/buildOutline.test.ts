import { describe, expect, it } from "bun:test";
import { buildOutline, firstSentence, summarizeReasons, type OutlineRow } from "./buildOutline";
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

  it("heads a closure passed to an open project call, after the callee's body", () => {
    const closure = { id: "5:9", kind: "closure", text: "withLock(() -> …)" };
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [
          step({ line: 5, col: 9, text: "withLock(() -> …)", calleeKey: "P#withLock", to: "withLock" }),
          step({ line: 5, col: 25, text: "pay(p)", calleeKey: "P#pay", to: "pay", frames: [closure] }),
        ],
        withLock: [step({ text: "lock.lock()", calleeKey: "Lock#lock" })],
        pay: [],
      },
    };
    const input = { rootId: "a", nodes: nodes("a", "withLock", "pay"), steps, toggled: {} };
    expect(show(buildOutline(input))).toEqual([
      "0 method P.a",
      "1 call withLock(() -> …)",
      "2 call lock.lock()",
      "1 frame withLock(() -> …)",
      "2 call pay(p)",
    ]);
    // Closed, the call's row is its closure's heading.
    const key = buildOutline(input)[1].key;
    expect(show(buildOutline({ ...input, toggled: { [key]: false } }))).toEqual([
      "0 method P.a",
      "1 call withLock(() -> …)",
      "2 call pay(p)",
    ]);
  });

  it("heads a closure once when a later argument's call was drawn between", () => {
    const closure = { id: "3:5", kind: "closure", text: "X(arg(), () -> …)" };
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [
          step({ line: 3, col: 5, text: "X(arg(), () -> …)", calleeKey: "Lib#X" }),
          step({ line: 3, col: 7, text: "arg()", calleeKey: "Lib#arg" }),
          step({ line: 3, col: 20, text: "y()", calleeKey: "Lib#y", frames: [closure] }),
          step({ line: 3, col: 26, text: "z()", calleeKey: "Lib#z", frames: [closure] }),
        ],
      },
    };
    // Without the heading y() would read as running inside arg().
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call X(arg(), () -> …)",
      "1 call arg()",
      "1 frame X(arg(), () -> …)",
      "2 call y()",
      "2 call z()",
    ]);
  });

  it("keeps a row's open state when a step is added before it", () => {
    const before: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "b()", calleeKey: "P#b", to: "b" })], b: [step({ text: "send()", calleeKey: "K#send" })], c: [step({ text: "get()", calleeKey: "R#get" })] },
    };
    const first = buildOutline({ rootId: "a", nodes: nodes("a", "b", "c"), steps: before, toggled: {} });
    const toggled = { [first[1].key]: false };
    const after: CodeGraphSteps = { ...before, steps: { ...before.steps, a: [step({ text: "c()", calleeKey: "P#c", to: "c" }), ...before.steps.a] } };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "b", "c"), steps: after, toggled });
    expect(show(rows)).toEqual(["0 method P.a", "1 call c()", "2 call get()", "1 call b()"]);
    expect(rows.find((r) => r.kind === "call" && r.title === "b()")).toMatchObject({ open: false, expandable: true });
  });

  it("keeps a fold's open state when a step is added before it", () => {
    const fold = (pre: CodeGraphStep[]): CodeGraphSteps => ({
      legacy: false, truncated: [],
      steps: { a: [...pre, step({ text: "send()", calleeKey: "K#send" }), step({ text: "log.a()", calleeKey: "Log#info", visible: false, reason: "logging" })] },
    });
    const first = buildOutline({ rootId: "a", nodes: nodes("a"), steps: fold([]), toggled: {} });
    const toggled = { [first.find((r) => r.kind === "fold")!.key]: true };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a"), steps: fold([step({ text: "get()", calleeKey: "R#get" })]), toggled });
    expect(rows.find((r) => r.kind === "fold")).toMatchObject({ open: true });
  });

  it("lists only implementations that are in the graph, and no empty row when none is", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [step({ text: "decide(x)", to: "decide", impls: ["gone"] }), step({ text: "route(x)", calleeKey: "P#route", to: "route", impls: ["gone", "implA"] })],
        decide: [], route: [], implA: [],
      },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "decide", "route", "implA"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 call decide(x)", "1 call route(x)", "2 method P.implA"]);
    expect(rows[1]).toMatchObject({ expandable: false, open: false });
  });

  it("lists an overridden method with a body of its own first, before its overrides", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "handle(x)", to: "handle", impls: ["special"] })], handle: [step({ text: "send()", calleeKey: "K#send" })], special: [] },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "handle", "special"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 call handle(x)", "2 method P.handle", "3 call send()", "2 method P.special"]);
    expect(rows[2]).toMatchObject({ impl: true, base: true, nodeId: "handle" });
    expect(rows[4]).toMatchObject({ impl: true, nodeId: "special" });
    expect(rows[4]).not.toHaveProperty("base");
  });

  it("points an implementation back to where it was drawn, and marks one that recurses", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [step({ text: "implA()", calleeKey: "P#implA", to: "implA" }), step({ text: "decide(x)", to: "decide", impls: ["implA", "implB"] })],
        decide: [], implA: [step({ text: "send()", calleeKey: "K#send" })],
        implB: [step({ text: "decide(y)", to: "decide", impls: ["implB"] })],
      },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "decide", "implA", "implB"), steps, toggled: {} });
    expect(show(rows)).toEqual([
      "0 method P.a",
      "1 call implA()",
      "2 call send()",
      "1 call decide(x)",
      "2 method P.implA",
      "2 method P.implB",
      "3 call decide(y)",
      "4 method P.implB",
    ]);
    expect(rows[4]).toMatchObject({ ref: { reason: "shown-above", key: rows[1].key }, open: false, expandable: false });
    expect(rows[7]).toMatchObject({ ref: { reason: "recursion", key: rows[5].key }, open: false, expandable: false });
  });

  it("says shown-above for a later call to a drawn method even where calls start closed", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "q()", calleeKey: "P#q", to: "q" }), step({ text: "b()", calleeKey: "P#b", to: "b" })], b: [step({ text: "q()", calleeKey: "P#q", to: "q" })], q: [step({ text: "send()", calleeKey: "K#send" })] },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "b", "q"), steps, toggled: {}, depth: 2 });
    expect(show(rows)).toEqual(["0 method P.a", "1 call q()", "2 call send()", "1 call b()", "2 call q() [shown-above]"]);
    expect(rows[4]).toMatchObject({ expandable: false, open: false });
  });

  it("draws a frame again with its own key when a method comes back into it", () => {
    const iff = { id: "3:5", kind: "if", text: "if ok" };
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "a1()", calleeKey: "A#1", frames: [iff] }), step({ text: "b()", calleeKey: "B#b" }), step({ text: "a2()", calleeKey: "A#2", frames: [iff] })] },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 frame if ok", "2 call a1()", "1 call b()", "1 frame if ok", "2 call a2()"]);
    expect(rows[4].key).toBe(`${rows[1].key}~2`);
  });

  it("closes a call that is open by default when the person closes it", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: [], steps: { a: [step({ text: "b()", to: "b" })], b: [step({ text: "send()" })] } };
    const input = { rootId: "a", nodes: nodes("a", "b"), steps, toggled: {} };
    const key = buildOutline(input)[1].key;
    const rows = buildOutline({ ...input, toggled: { [key]: false } });
    expect(show(rows)).toEqual(["0 method P.a", "1 call b()"]);
    expect(rows[1]).toMatchObject({ open: false, expandable: true });
  });

  it("draws only the root's row when the root is closed", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: [], steps: { a: [step({ text: "x()" })] } };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: { root: false } });
    expect(rows).toEqual([expect.objectContaining({ kind: "method", key: "root", open: false, expandable: true })]);
  });

  it("expands a hidden project call inside an opened fold", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "fmt(x)", calleeKey: "P#fmt", to: "fmt", visible: false, reason: "helper" })], fmt: [step({ text: "pad()", calleeKey: "S#pad" })] },
    };
    const input = { rootId: "a", nodes: nodes("a", "fmt"), steps, toggled: {} };
    const foldKey = buildOutline(input)[1].key;
    const rows = buildOutline({ ...input, toggled: { [foldKey]: true } });
    expect(show(rows)).toEqual(["0 method P.a", "1 fold +1 · helper", "2 call fmt(x)", "3 call pad()"]);
    expect(rows[2]).toMatchObject({ hidden: true, open: true, targetId: "fmt" });
  });

  it("opens as many levels of methods as asked, the root included", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: [], steps: { a: [step({ text: "b()", to: "b" })], b: [step({ text: "c()", to: "c" })], c: [step({ text: "d()" })] } };
    const at = (depth: number) => show(buildOutline({ rootId: "a", nodes: nodes("a", "b", "c"), steps, toggled: {}, depth }));
    expect(at(1)).toEqual(["0 method P.a", "1 call b()"]);
    expect(at(2)).toEqual(["0 method P.a", "1 call b()", "2 call c()"]);
  });
});

describe("summarizeReasons", () => {
  it("names the three most frequent reasons, ties in first-seen order", () => {
    expect(summarizeReasons(["logging", "metrics", "helper", "metrics", "tracing", "helper", "metrics"])).toBe("metrics, helper, logging");
    expect(summarizeReasons(["b", "a"])).toBe("b, a");
    expect(summarizeReasons([])).toBe("");
  });
});

describe("firstSentence", () => {
  it("keeps the first sentence of a doc comment", () => {
    expect(firstSentence("  Forwards the record. Then counts it.")).toBe("Forwards the record.");
    expect(firstSentence("Is it on? Maybe.")).toBe("Is it on?");
    expect(firstSentence("Reads v1.2 of the schema")).toBe("Reads v1.2 of the schema");
    expect(firstSentence("")).toBe("");
  });
});
