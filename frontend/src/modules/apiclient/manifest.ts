import { lazy, Suspense, createElement } from "react";
import { Send } from "lucide-react";
import { UTILITIES_GROUP } from "../groups";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";
import { registerToolResultView } from "@/workbench/toolResultViews";
import { parseEndpointDraft } from "./endpointDraftResult";

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
  commands: [{ id: "apiclient.new_request", title: "New request" }],
  views: [
    {
      id: "apiclient.request",
      title: "Request",
      ...lazyModuleView(() =>
        import("./ApiRequestView").then(({ ApiRequestView }) => ({ default: ApiRequestView })),
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
  return createElement(
    Suspense,
    { fallback: null },
    createElement(LazyEndpointDraftResultCard, { draft: parsed.draft, endpointId: parsed.endpointId }),
  );
});
