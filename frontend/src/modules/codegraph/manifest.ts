import { lazy, createElement } from "react";
import { Code, FileCode2, ListChecks, Workflow } from "lucide-react";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { parseApiFlow } from "./apiFlowResult";
import { parseEntryPointMatches, parseSymbolMatches } from "./searchResult";
import { revealFileRef } from "./revealFile";

export const codegraphModule: ModuleManifest = {
  id: "codegraph",
  title: "Code Graph",
  // The explorer shows the workspace's folders. Its home costs nothing to open, so it opens
  // rather than showing "has nothing open" (ModuleManifest.opensHome).
  opensHome: true,
  version: "1.0.0",
  apiVersion: "1",
  icon: Code,
  // Documents own their toolbars, scrolling and status bar; fill the shell card.
  padded: false,
  // A flow, a file and the plan are documents you want side by side, so
  // each is a tab (documents.ts) and the strip is drawn.
  tabs: true,
  capabilities: ["codegraph:index", "codegraph:read", "codegraph:toolchain"],
  // The actions are imported when a command runs, not with the manifest: the
  // manifest is in the initial bundle, and Code Graph's store and bridge are
  // not.
  commands: [
    {
      id: "codegraph.find_in_workspace",
      title: "Find in workspace…",
      run: () => void import("./codegraphActions").then((a) => a.openWorkspaceSearch()),
    },
    {
      // Named for the palette, which lists every module's commands together:
      // beside Database's "Re-index schema", a bare "Re-index" says nothing.
      id: "codegraph.reindex_scope",
      title: "Re-index Code Graph folders in scope",
      run: () => void import("./codegraphActions").then((a) => a.reindexScope()),
    },
    {
      id: "codegraph.reindex_workspace",
      title: "Re-index Code Graph workspace",
      run: () => void import("./codegraphActions").then((a) => a.reindexWorkspace()),
    },
    {
      id: "codegraph.go_back",
      title: "Code Graph: Go back",
      run: () => void import("./codegraphActions").then((a) => a.goBack()),
    },
    {
      id: "codegraph.go_forward",
      title: "Code Graph: Go forward",
      run: () => void import("./codegraphActions").then((a) => a.goForward()),
    },
  ],
  // The strip's +: a new tab here has no blank form to fill, so it starts the
  // thing you do first in a workspace — look for something.
  newTab: () => void import("./codegraphActions").then((a) => a.openWorkspaceSearch()),
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
      // By ids when the caller resolved it (Agent Hub references), else by project.
      return revealFileRef(ref, placement, actions);
    case "endpoint": {
      // "endpoint" is the ref kind agents and saved transcripts use for any
      // entry point, HTTP or not. It is looked for in the folder the ref names,
      // else in whichever folder of the workspace has it.
      const found = await actions.findEntryPoint(ref.id, ref.project);
      if (found) actions.viewFlow(found.owner, found.entry, { preview: false, beside });
      return;
    }
    case "node": {
      const owner = await actions.ownerOfNode(ref.project);
      if (!owner) return;
      const { openNodeFlow } = await import("./documents");
      openNodeFlow(owner, ref.id, ref.label ?? ref.id, { preview: false, beside });
      void actions.loadFlow(owner.folderId, ref.id);
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
  return createElement(LazyApiFlowResultCard, { flow: parsed.flow, entryPointId: parsed.entryPointId });
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
  return createElement(LazySymbolMatchResultCard, { matches });
});

const LazyEntryPointListResultCard = lazy(() =>
  import("./EntryPointListResultCard").then(({ EntryPointListResultCard }) => ({ default: EntryPointListResultCard })),
);
registerToolResultView("codegraph.listEndpoints", ({ rawOutput }) => {
  const entries = parseEntryPointMatches(rawOutput);
  if (!entries) return null;
  return createElement(LazyEntryPointListResultCard, { entries });
});
