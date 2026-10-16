import { expect, it } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { entryCollapseKeys } from "./treeActions";
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

// A tinted chip is only 4.5:1 at full strength (themeContrast.test), so an
// off chip is the neutral one rather than a faded tint.
it("verb filter chips keep their tint at full strength and turn neutral when filtered out", () => {
  const entry = (id: string, verb: string) => ({ id, entryNode: id, verb, target: `/${id}`, kind: "http" as const, owner: "C", group: "C", service: "", order: 0 });
  const folders: FolderEntries[] = [{
    folderId: "f", path: "/fixture", label: "api", note: "",
    health: { health: "ready", tone: "ok", label: "Indexed", detail: "" },
    entries: [entry("a", "GET"), entry("b", "POST")],
  }];
  const chip = (html: string, verb: string) =>
    html.match(new RegExp(`<button[^>]*aria-pressed="(true|false)"[^>]*class="([^"]*)"[^>]*>${verb}</button>`))?.slice(1) ?? [];
  const filtered = renderToString(
    <TooltipProvider>
      <EntryPointsTab folders={folders} query="" selected={null} verbFilter={["GET"]} collapsed={[]}
        onVerbFilter={noop} onToggleGroup={noop} onSelect={noop} onKeep={noop}
        onOpenBeside={noop} onAskAgent={noop} onReindexFolder={noop} />
    </TooltipProvider>,
  );
  const [getPressed, getClass] = chip(filtered, "GET");
  const [postPressed, postClass] = chip(filtered, "POST");
  expect(getPressed).toBe("true");
  expect(getClass).toContain("text-(--hue-green)");
  expect(postPressed).toBe("false");
  expect(postClass).toContain("bg-surface-raised");
  expect(postClass).not.toContain("--hue-yellow");
  const [, restClass] = chip(render(folders), "POST");
  expect(restClass).toContain("text-(--hue-yellow)");
  for (const cls of [getClass, postClass, restClass]) expect(cls).not.toMatch(/(^|\s)(hover:)?opacity-/);
});

it("keeps owner groups folded after reopening a collapsed workspace folder", () => {
  const ep = { id: "health", entryNode: "Health", verb: "GET", target: "/health", kind: "http", owner: "Health", group: "HealthController", service: "api", order: 0 };
  const folders: FolderEntries[] = ["a", "b"].map((folderId) => ({
    folderId, path: `/${folderId}`, label: folderId, note: "",
    health: { health: "ready", tone: "ok", label: "Indexed", detail: "" }, entries: [ep],
  }));
  const collapsed = entryCollapseKeys(folders);
  const draw = (keys: string[]) => renderToString(
    <TooltipProvider>
      <EntryPointsTab folders={folders} query="" selected={null} verbFilter={[]} collapsed={keys}
        onVerbFilter={noop} onToggleGroup={noop} onSelect={noop} onKeep={noop}
        onOpenBeside={noop} onAskAgent={noop} onReindexFolder={noop} />
    </TooltipProvider>,
  );
  expect(draw(collapsed)).not.toContain("HealthController");
  const reopened = draw(collapsed.filter((key) => key !== "folder:a"));
  expect(reopened).toContain("HealthController");
  expect(reopened).not.toContain("/health");
});
