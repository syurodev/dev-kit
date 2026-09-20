import { Send } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

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
