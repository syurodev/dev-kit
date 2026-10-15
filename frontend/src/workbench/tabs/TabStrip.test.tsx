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
