import { KeyRound } from "lucide-react";
import { lazyModuleView, type ModuleManifest } from "../registry";

export const accountsModule: ModuleManifest = {
  id: "accounts",
  title: "Accounts",
  version: "1.0.0",
  apiVersion: "1",
  icon: KeyRound,
  padded: false,
  // accounts:reveal is declared because the capability exists; the grant that
  // matters lives in the container, and MCP never receives it.
  capabilities: ["accounts:read", "accounts:write", "accounts:reveal", "accounts:use"],
  commands: [{ id: "accounts.create", title: "New account" }],
  views: [
    {
      id: "accounts.list",
      title: "Accounts",
      ...lazyModuleView(() =>
        import("./AccountsView").then(({ AccountsView }) => ({ default: AccountsView })),
      ),
    },
  ],
};
