import { afterAll, beforeEach, describe, expect, it, mock } from "bun:test";
import type { CallVisibilityMode, CodeGraphSteps, CodeGraphSubgraph } from "@/bridge/codegraph";

/**
 * The flow actions against a fake graph bridge: each call to the steps query
 * waits until the test answers it, so the tests can replace a subgraph while
 * its steps are still on their way.
 *
 * bun's mock.module applies to the whole run, so the real modules are spread,
 * the steps and visibility fakes answer only while this file's tests run
 * (`faking`, cleared in afterAll) and the graph fake answers the way
 * fileActions.test.ts's does.
 */
mock.module("@/bridge/uistate", () => {
  const mem = new Map<string, unknown>();
  return {
    getUIState: async (key: string) => String(mem.get(key) ?? ""),
    setUIState: async (key: string, value: string) => void mem.set(key, value),
    uiStateStorage: {
      getItem: (key: string) => mem.get(key) ?? null,
      setItem: (key: string, value: unknown) => void mem.set(key, value),
      removeItem: (key: string) => void mem.delete(key),
    },
  };
});

type Pending = { nodeIds: string[]; resolve: (s: CodeGraphSteps) => void; reject: (e: unknown) => void };
const stepCalls: Pending[] = [];
let neighbors: (root: string, continuation?: string) => Promise<CodeGraphSubgraph> = async () => ({ nodes: [], edges: [], truncated: false, continuation: "" });
let saveVisibility: (key: string, mode: string) => Promise<void> = async () => {};
const toastErrors: string[] = [];
let faking = false;

const realGraph = await import("@/bridge/codegraph");
mock.module("@/bridge/codegraph", () => ({
  ...realGraph,
  getCodeGraphNeighbors: (root: string, _depth: number, filter?: { continuation?: string }) => neighbors(root, filter?.continuation),
  getCodeGraphSteps: (nodeIds: string[], servicePath?: string) =>
    faking
      ? new Promise<CodeGraphSteps>((resolve, reject) => stepCalls.push({ nodeIds, resolve, reject }))
      : realGraph.getCodeGraphSteps(nodeIds, servicePath),
  setCallVisibility: (key: string, mode: CallVisibilityMode) =>
    faking ? saveVisibility(key, mode) : realGraph.setCallVisibility(key, mode),
}));

const realSonner = await import("sonner");
mock.module("sonner", () => ({
  ...realSonner,
  toast: Object.assign((...args: Parameters<typeof realSonner.toast>) => realSonner.toast(...args), realSonner.toast, {
    error: (message: unknown) => {
      toastErrors.push(String(message));
      return 0;
    },
  }),
}));

const { useCodeGraphStore } = await import("./codegraphStore");
const actions = await import("./flowActions");
const { flowKey } = await import("./workspace/identity");
const { documentsOf, openEntryPointFlow, openNodeFlow } = await import("./documents");

const WS = "w1";
const FOLDER = "fa";
const settle = () => new Promise((r) => setTimeout(r, 0));
const node = (id: string) => ({ id, label: `acme.P.${id}`, kind: "method", layer: "", filePath: "P.java", line: 1, endLine: 2, summary: "" });
const graph = (ids: string[], continuation = ""): CodeGraphSubgraph => ({ nodes: ids.map(node), edges: [], truncated: !!continuation, continuation });
const steps = (marker: string): CodeGraphSteps => ({ steps: { [marker]: [] }, truncated: [], legacy: false });
const flow = (root: string) => useCodeGraphStore.getState().flows[flowKey(FOLDER, root)];

afterAll(() => {
  faking = false;
});

beforeEach(() => {
  faking = true;
  stepCalls.length = 0;
  toastErrors.length = 0;
  neighbors = async () => graph([]);
  saveVisibility = async () => {};
  useCodeGraphStore.setState({
    workspaceId: WS,
    folders: [{ id: FOLDER, workspace_id: WS, name: FOLDER, path: `/ws/${FOLDER}`, name_source: "" }],
    primaryId: FOLDER,
    flows: {},
  } as never);
});

describe("flow steps", () => {
  it("loads the steps of a flow's nodes once its graph lands, and opens on the outline", async () => {
    neighbors = async () => graph(["r1", "b"]);
    await actions.loadFlow(FOLDER, "r1");
    expect(stepCalls.map((c) => c.nodeIds)).toEqual([["r1", "b"]]);
    expect(flow("r1")).toMatchObject({ stepsLoading: true, mode: "outline", toggled: {} });
    stepCalls[0].resolve(steps("first"));
    await settle();
    expect(flow("r1")).toMatchObject({ steps: steps("first"), stepsLoading: false, stepsError: "" });
  });

  it("drops steps asked for a subgraph a refresh has replaced", async () => {
    neighbors = async () => graph(["r2"]);
    await actions.loadFlow(FOLDER, "r2");
    neighbors = async () => graph(["r2", "c"]);
    await actions.loadFlow(FOLDER, "r2", true);
    expect(stepCalls.map((c) => c.nodeIds)).toEqual([["r2"], ["r2", "c"]]);

    stepCalls[0].resolve(steps("stale"));
    await settle();
    expect(flow("r2").steps).toBeNull();
    expect(flow("r2").stepsLoading).toBe(true);

    stepCalls[1].resolve(steps("fresh"));
    await settle();
    expect(flow("r2")).toMatchObject({ steps: steps("fresh"), stepsLoading: false });
  });

  it("stops loading when the subgraph its steps were for is gone and nothing replaced it", async () => {
    neighbors = async () => graph(["r3"]);
    await actions.loadFlow(FOLDER, "r3");
    // The node went away in a re-index: the graph fails and no new steps are asked for.
    neighbors = async () => {
      throw new Error("node not found");
    };
    await actions.loadFlow(FOLDER, "r3", true);
    expect(stepCalls).toHaveLength(1);

    stepCalls[0].resolve(steps("orphan"));
    await settle();
    expect(flow("r3")).toMatchObject({ subgraph: null, steps: null, stepsLoading: false });
  });

  it("re-reads the steps after the next page of the graph is merged in", async () => {
    neighbors = async (_root, continuation) => (continuation ? graph(["d"]) : graph(["r4"], "next"));
    await actions.loadFlow(FOLDER, "r4");
    await actions.loadMoreNeighbors(FOLDER, "r4");
    expect(stepCalls.map((c) => c.nodeIds)).toEqual([["r4"], ["r4", "d"]]);
  });

  it("keeps the mode and the rows the person toggled", () => {
    actions.setFlowMode(FOLDER, "r5", "graph");
    actions.toggleOutlineRow(FOLDER, "r5", "root/x#0", false);
    actions.toggleOutlineRow(FOLDER, "r5", "root/y#0", true);
    expect(flow("r5")).toMatchObject({ mode: "graph", toggled: { "root/x#0": false, "root/y#0": true } });
  });
});

describe("call visibility", () => {
  it("says why a choice was not saved and re-reads nothing", async () => {
    saveVisibility = async () => {
      throw new Error("preferences are read-only");
    };
    await actions.setCallVisibility("Log#info", "show");
    expect(toastErrors).toEqual(["preferences are read-only"]);
    expect(stepCalls).toHaveLength(0);
  });

  it("re-reads each open flow once, however many tabs show it", async () => {
    neighbors = async (root) => graph([root]);
    const owner = { workspaceId: WS, folderId: FOLDER };
    const entry = { id: "e6", verb: "GET", target: "/r6", entryNode: "r6", kind: "http", service: "", owner: "", group: "", order: 0 };
    openEntryPointFlow(owner, entry as never, { preview: false });
    openNodeFlow(owner, "r6", "P.r6", { preview: false });
    expect(documentsOf().filter((d) => d.doc.kind === "flow" && d.doc.root === "r6")).toHaveLength(2);
    await actions.loadFlow(FOLDER, "r6");
    stepCalls.length = 0;

    const saved: string[] = [];
    saveVisibility = async (key, mode) => void saved.push(`${key}=${mode}`);
    const done = actions.setCallVisibility("Log#info", "show");
    await settle();
    expect(saved).toEqual(["Log#info=show"]);
    expect(stepCalls.filter((c) => c.nodeIds.includes("r6"))).toHaveLength(1);
    for (const c of stepCalls) c.resolve(steps("again"));
    await done;
  });
});
