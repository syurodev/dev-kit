import { Send } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";
import { registerIntent } from "@/workbench/intents";

export const apiclientModule: ModuleManifest = {
  id: "apiclient",
  title: "API Client",
  version: "1.0.0",
  apiVersion: "1",
  icon: Send,
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
