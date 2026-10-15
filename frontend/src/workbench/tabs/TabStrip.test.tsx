import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { WorkbenchTab } from "../../stores/workbenchTabs";
import { TabStrip } from "./TabStrip";

// Module "m" is never registered, so getModule() is undefined and no preview
// reads as a hidden home preview.
function renderStrip(tabs: WorkbenchTab[], activeTabId: string): string {
  return renderToString(
    <TooltipProvider>
      <TabStrip
        tabs={tabs}
        activeTabId={activeTabId}
        onSelect={() => {}}
        onClose={() => {}}
        onReorder={() => {}}
        keyboardBrowsing={false}
        onExitBrowsing={() => {}}
      />
    </TooltipProvider>,
  );
}

test("a preview tab has a close button", () => {
  const html = renderStrip([
    { id: "m:v:a", moduleId: "m", viewId: "v.doc", title: "Kept" },
    { id: "m:v:b", moduleId: "m", viewId: "v.doc", title: "Peek", preview: true },
  ], "m:v:a");
  expect(html).toContain('aria-label="Close Peek"');
  expect(html).toContain('aria-label="Close Kept"');
});

test("a strip that does not overflow has no scroll buttons and no fade", () => {
  // Static render: no layout, so the strip never measures an overflow. The
  // arithmetic that decides one is tabStripModel's overflowEdges/scrollStep.
  const html = renderStrip([
    { id: "m:v:a", moduleId: "m", viewId: "v.doc", title: "One" },
    { id: "m:v:b", moduleId: "m", viewId: "v.doc", title: "Two" },
  ], "m:v:a");
  expect(html).not.toContain("Scroll tabs");
  expect(html).not.toContain("mask-image");
});

const tab = (id: string, extra: Partial<WorkbenchTab> = {}): WorkbenchTab => ({
  id, moduleId: "m", viewId: "v.doc", title: id, ...extra,
});

/** The markup of one tab, from its opening tag to the next tab's. */
function tabMarkup(html: string, id: string): string {
  const start = html.indexOf(`data-tab-id="${id}"`);
  const next = html.indexOf('role="tab"', start + 1);
  return html.slice(start, next === -1 ? undefined : next);
}

test("a running tab spins and a failed one shows the error dot, active or not", () => {
  const html = renderStrip([tab("a"), tab("run", { status: "running" }), tab("err", { status: "error" })], "a");
  expect(tabMarkup(html, "run")).toContain("tab-status-spin");
  expect(tabMarkup(html, "err")).toContain("bg-(--status-error)");
  expect(tabMarkup(html, "a")).not.toContain("tab-status-spin");
});

test("success shows on a tab not yet looked at, never on the active one", () => {
  const html = renderStrip([tab("done", { status: "success" }), tab("other", { status: "success" })], "done");
  expect(tabMarkup(html, "done")).not.toContain("bg-(--status-success)");
  expect(tabMarkup(html, "other")).toContain("bg-(--status-success)");
});

test("a dirty tab shows the unsaved dot and keeps its close button for hover", () => {
  const html = renderStrip([tab("a"), tab("edited", { dirty: true, title: "Edited" })], "a");
  const markup = tabMarkup(html, "edited");
  expect(markup).toContain("data-tab-dirty");
  expect(markup).toContain('aria-label="Close Edited"');
  expect(markup).toContain("Unsaved changes");
  expect(tabMarkup(html, "a")).not.toContain("data-tab-dirty");
});

test("a dirty tab's close button stays in the accessibility tree, only out of sight", () => {
  const html = renderStrip([tab("a"), tab("edited", { dirty: true, title: "Edited" })], "a");
  const close = tabMarkup(html, "edited").match(/aria-label="Close Edited" class="([^"]*)"/)?.[1] ?? "";
  expect(close).not.toMatch(/(^|\s)hidden(\s|$)/);
  expect(close).toContain("opacity-0");
  expect(close).toContain("group-hover:opacity-100");
});
