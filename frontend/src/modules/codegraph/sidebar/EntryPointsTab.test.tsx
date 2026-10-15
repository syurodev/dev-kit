import { expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { EntryPointsTab, type FolderEntries } from "./EntryPointsTab";

const noop = () => {};
const render = (folders: FolderEntries[]) => renderToString(
  <TooltipProvider>
    <EntryPointsTab folders={folders} query="" selected={null} verbFilter={[]} collapsed={[]}
      onVerbFilter={noop} onToggleGroup={noop} onSelect={noop} onKeep={noop}
      onOpenBeside={noop} onAskAgent={noop} onReindexFolder={noop} />
  </TooltipProvider>,
);

it("shows route ID, source topic and consumer group with the message entry", () => {
  const html = render([{
    folderId: "f", path: "/fixture", label: "dispatcher", note: "",
    health: { health: "ready", tone: "ok", label: "Indexed", detail: "Source-resolved Camel routes." },
    entries: [{ id: "a", entryNode: "route", verb: "MESSAGE", target: "kafka:{domain.source().topic()}", kind: "message",
      routeId: "webhook-dispatch-{domain.name()}", topic: "{domain.source().topic()}", consumerGroup: "{source.groupId()}.{domain.name()}",
      owner: "WebhookRoutes", group: "WebhookRoutes", service: "", order: 0 }],
  }]);
  expect(html).toContain("webhook-dispatch-");
  expect(html).toContain("domain.source().topic()");
  expect(html).toContain(" · group {source.groupId()}");
  expect(html).toContain("source.groupId()");
});

it("keeps support warnings visible for one folder even when entries exist", () => {
  const html = render([{
    folderId: "f", path: "/fixture", label: "dispatcher", note: "",
    health: { health: "limited", tone: "warn", label: "Indexed · entry limits", detail: "Camel route handler is unresolved: dispatch" },
    entries: [{ id: "a", entryNode: "route", verb: "MESSAGE", target: "kafka:events", kind: "message", owner: "Routes", group: "Routes", service: "", order: 0 }],
  }]);
  expect(html).toContain("indexed with entry point limits");
  expect(html).toContain("handler is unresolved");
});

// The note sits in a fixed-height tree row: wrapped, a long detail spilled
// over the folders below it. One line, the whole text in its tooltip.
it("keeps a folder's long note on its own row", () => {
  const detail = "Java entry points: Spring HTTP mappings and source-resolved Camel Java routes. Dynamic DSLs and runtime configuration may be unresolved.";
  const html = render([{
    folderId: "f", path: "/fixture", label: "audit", note: "",
    health: { health: "limited", tone: "warn", label: "Indexed · entry limits", detail },
    entries: [],
  }]);
  expect(html).not.toContain("whitespace-normal");
  expect(html).toMatch(/class="[^"]*\btruncate\b[^"]*"[^>]*>No supported entry points found\./);
  expect(html).toContain(`title="No supported entry points found. ${detail}"`);
});

it("shows consumer group even when Camel has no explicit route ID", () => {
  const html = render([{
    folderId: "f", path: "/fixture", label: "dispatcher", note: "",
    health: { health: "ready", tone: "ok", label: "Indexed", detail: "" },
    entries: [{ id: "a", entryNode: "route", verb: "MESSAGE", target: "kafka:events", consumerGroup: "standalone", kind: "message", owner: "Routes", group: "Routes", service: "", order: 0 }],
  }]);
  expect(html).toContain("kafka:events");
  expect(html).toContain(" · group standalone");
});
