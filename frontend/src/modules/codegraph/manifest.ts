import { lazy, Suspense, createElement } from "react";
import { Code, FileCode2, ListChecks, Workflow } from "lucide-react";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { parseApiFlow } from "./apiFlowResult";
import { parseEntryPointMatches, parseSymbolMatches } from "./searchResult";

export const codegraphModule: ModuleManifest = {
  id: "codegraph",
  title: "Code Graph",
  // The explorer shows the open project, or the picker when there is none. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: Code,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
  // A flow, a file and the plan are documents you want side by side, so
  // each is a tab (documents.ts) and the strip is drawn.
  tabs: true,
  capabilities: ["codegraph:index", "codegraph:read", "codegraph:toolchain"],
  // The actions are imported when a command runs, not with the manifest: the
  // manifest is in the initial bundle, and Code Graph's store and bridge are
  // not.
  commands: [
    {
      id: "codegraph.open_project",
      title: "Open project…",
      run: () => void import("./codegraphActions").then((actions) => actions.pickAndOpenProject()),
    },
    {
      // Named for the palette, which lists every module's commands together:
      // beside Database's "Re-index schema", a bare "Re-index" says nothing.
      id: "codegraph.reindex_project",
      title: "Re-index Code Graph project",
      run: () => void import("./codegraphActions").then((actions) => actions.reindexActiveProject()),
    },
  ],
  // The rail belongs to the module, not to whichever document is open: a
  // file tab and no flow still needs its entry points.
  sidebar: lazy(() => import("./CodeGraphSidebar").then(({ CodeGraphSidebar }) => ({ default: CodeGraphSidebar }))),
  views: [
    {
      id: "codegraph.explorer",
      title: "Code Graph",
      ...lazyModuleView(() => import("./CodeGraphHome").then(({ CodeGraphHome }) => ({ default: CodeGraphHome }))),
    },
    {
      id: "codegraph.flow",
      title: "Flow",
      icon: Workflow,
      ...lazyModuleView(() => import("./FlowDocument").then(({ FlowDocument }) => ({ default: FlowDocument }))),
    },
    {
      id: "codegraph.file",
      title: "File",
      icon: FileCode2,
      ...lazyModuleView(() => import("./FileDocument").then(({ FileDocument }) => ({ default: FileDocument }))),
    },
    {
      id: "codegraph.plan",
      title: "Plan",
      icon: ListChecks,
      ...lazyModuleView(() => import("./PlanDocument").then(({ PlanDocument }) => ({ default: PlanDocument }))),
    },
  ],
  paletteSearch: {
    noun: "entry points, files and symbols",
    search: (query, signal) => import("./paletteSearch").then((m) => m.searchPalette(query, signal)),
  },
};

/**
 * `codegraph.reveal` (docs/dev-kit/cross-module.mdx): show an entity of Code
 * Graph from anywhere — a file an agent wrote, a node a task links to —
 * without the caller importing this module's actions. Registered with the
 * manifest, which is eager; the actions load when first asked.
 */
registerIntent("codegraph.reveal", async (ref, { placement }) => {
  const actions = await import("./codegraphActions");
  const beside = placement === "beside";
  switch (ref.kind) {
    case "file":
      await actions.openFileInProject(ref.project ?? "", ref.id, ref.line, placement);
      return;
    case "endpoint": {
      const { useCodeGraphStore } = await import("./codegraphStore");
      // "endpoint" is the ref kind agents and saved transcripts use for any
      // entry point, HTTP or not.
      const ep = useCodeGraphStore.getState().entryPoints.find((e) => e.id === ref.id);
      if (ep) actions.viewFlow(ep, { preview: false, beside });
      return;
    }
    case "node": {
      const { openNodeFlow } = await import("./documents");
      openNodeFlow(ref.id, ref.label, { preview: false, beside });
      void actions.loadFlow(ref.id);
      return;
    }
  }
});

/**
 * ToolResultView for `codegraph.getApiFlow` (docs/dev-kit/tool-result-view.mdx).
 * The shape is checked synchronously, with no React involved, before the
 * card component is ever imported — a bad or truncated rawOutput returns
 * null (falls back to the transcript's plain JSON) without paying for the
 * lazy chunk.
 */
const LazyApiFlowResultCard = lazy(() =>
  import("./ApiFlowResultCard").then(({ ApiFlowResultCard }) => ({ default: ApiFlowResultCard })),
);
registerToolResultView("codegraph.getApiFlow", ({ rawOutput }) => {
  const parsed = parseApiFlow(rawOutput);
  if (!parsed) return null;
  return createElement(
    Suspense,
    { fallback: null },
    createElement(LazyApiFlowResultCard, { flow: parsed.flow, entryPointId: parsed.entryPointId }),
  );
});

/**
 * ToolResultView for `codegraph.searchSymbol` and `codegraph.listEndpoints`
 * (docs/dev-kit/tool-result-view.mdx) — both reuse the existing
 * `codegraph.reveal` intent per row, no new registry.
 */
const LazySymbolMatchResultCard = lazy(() =>
  import("./SymbolMatchResultCard").then(({ SymbolMatchResultCard }) => ({ default: SymbolMatchResultCard })),
);
registerToolResultView("codegraph.searchSymbol", ({ rawOutput }) => {
  const matches = parseSymbolMatches(rawOutput);
  if (!matches) return null;
  return createElement(Suspense, { fallback: null }, createElement(LazySymbolMatchResultCard, { matches }));
});

const LazyEntryPointListResultCard = lazy(() =>
  import("./EntryPointListResultCard").then(({ EntryPointListResultCard }) => ({ default: EntryPointListResultCard })),
);
registerToolResultView("codegraph.listEndpoints", ({ rawOutput }) => {
  const entries = parseEntryPointMatches(rawOutput);
  if (!entries) return null;
  return createElement(Suspense, { fallback: null }, createElement(LazyEntryPointListResultCard, { entries }));
});
