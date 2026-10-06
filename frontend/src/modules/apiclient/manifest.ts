import { lazy, createElement } from "react";
import { Send, Variable } from "lucide-react";
import { UTILITIES_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { registerApiActivities } from "./toolActivities";
import { parseEndpointDraft } from "./endpointDraftResult";
import { parseSendResult } from "./sendResult";

export const apiclientModule: ModuleManifest = {
  id: "apiclient",
  title: "API Client",
  version: "1.0.0",
  apiVersion: "1",
  icon: Send,
  group: UTILITIES_GROUP,
  padded: false,
  tabs: true,
  // api:send is separate from api:read and api:write: reading a request and
  // firing it at a production host are not the same permission.
  capabilities: ["api:read", "api:write", "api:send"],
  // Collections, folders, requests and environments are the module's, not a
  // tab's: they are there with nothing open, which is how the module starts.
  sidebar: lazy(() => import("./ApiClientSidebar").then(({ ApiClientSidebar }) => ({ default: ApiClientSidebar }))),
  // The strip's + is a new request, the way it is in every HTTP client.
  newTab: () => {
    void import("./documents").then(({ openNewRequest }) => openNewRequest());
  },
  commands: [
    {
      id: "apiclient.new_request",
      title: "New request",
      run: () => {
        void import("./documents").then(({ openNewRequest }) => openNewRequest());
      },
    },
    {
      id: "apiclient.import",
      title: "Import cURL or Postman…",
      run: () => {
        void import("./importCommand").then(({ openImport }) => openImport());
      },
    },
  ],
  views: [
    {
      id: "apiclient.request",
      title: "Request",
      ...lazyModuleView(() =>
        import("./ApiRequestView").then(({ ApiRequestView }) => ({ default: ApiRequestView })),
      ),
    },
    {
      id: "apiclient.variables",
      title: "Variables",
      icon: Variable,
      ...lazyModuleView(() =>
        import("./VariablesView").then(({ VariablesView }) => ({ default: VariablesView })),
      ),
    },
  ],
};


/**
 * `api.fromEndpoint` (docs/dev-kit/cross-module.mdx): Try in API Client, from
 * a Code Graph endpoint, without Code Graph importing this module's views.
 *
 * Beside by default, which is the example the intents module itself gives: the
 * endpoint being read stays on screen while the draft opens next to it.
 */
registerIntent(
  "api.fromEndpoint",
  async (ref, { placement }) => {
    if (ref.kind !== "endpoint") return;
    const { openEndpointDraft } = await import("./documents");
    openEndpointDraft(ref.id, ref.label, { beside: placement === "beside" });
  },
  "beside",
);

/**
 * ToolResultView for `api.fromEndpoint` (docs/dev-kit/tool-result-view.mdx).
 * Shape checked synchronously before the card component loads — a bad
 * rawOutput, or a rawInput missing endpointId, falls back to plain JSON.
 */
const LazyEndpointDraftResultCard = lazy(() =>
  import("./EndpointDraftResultCard").then(({ EndpointDraftResultCard }) => ({ default: EndpointDraftResultCard })),
);
registerToolResultView("api.fromEndpoint", ({ rawInput, rawOutput }) => {
  const parsed = parseEndpointDraft(rawInput, rawOutput);
  if (!parsed) return null;
  return createElement(LazyEndpointDraftResultCard, { draft: parsed.draft, endpointId: parsed.endpointId });
});

/**
 * `api.reveal` (docs/dev-kit/cross-module.mdx): open one saved request from
 * anywhere — a ToolResultView card for `api.send` — without the caller
 * importing this module's documents helper. `kind: "request"` matches
 * internal/agenthub/attachedref.go's own ref kind for a saved request.
 */
registerIntent(
  "api.reveal",
  async (ref, { placement }) => {
    if (ref.kind !== "request") return;
    const { openRequest } = await import("./documents");
    openRequest(ref.id, ref.label || ref.id, { beside: placement === "beside" });
  },
  "beside",
);

/**
 * ToolResultView for `api.send` (docs/dev-kit/tool-result-view.mdx). Shape
 * checked synchronously before the card component loads — a rawInput
 * missing requestId, or a rawOutput that doesn't look like a Response,
 * falls back to plain JSON.
 */
const LazySendResultCard = lazy(() =>
  import("./SendResultCard").then(({ SendResultCard }) => ({ default: SendResultCard })),
);
registerToolResultView("api.send", ({ rawInput, rawOutput, heading }) => {
  const parsed = parseSendResult(rawInput, rawOutput);
  if (!parsed) return null;
  return createElement(LazySendResultCard, { response: parsed.response, requestId: parsed.requestId, heading });
});

// What this module's tools that change something do, for the transcript's
// always-visible cards (workbench/toolActivity.ts).
registerApiActivities();
