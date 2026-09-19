import { lazy } from "react";
import { FileCode2, ListChecks, Network, Workflow } from "lucide-react";
import { registerIntent } from "@/workbench/intents";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const codegraphModule: ModuleManifest = {
  id: "codegraph",
  title: "Code Graph",
  version: "1.0.0",
  apiVersion: "1",
  icon: Network,
  // Keeps the shell's padding until this module owns its own chrome.
  padded: true,
  // A flow, a file and the plan are documents you want side by side, so
  // each is a tab (documents.ts) and the strip is drawn.
  tabs: true,
  capabilities: ["codegraph:index", "codegraph:read", "codegraph:toolchain"],
  commands: [
    {
      id: "codegraph.open_project",
      title: "Open Java project",
    },
  ],
  // The rail belongs to the module, not to whichever document is open: a
  // file tab and no flow still needs its endpoints.
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
      const ep = useCodeGraphStore.getState().endpoints.find((e) => e.id === ref.id);
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
