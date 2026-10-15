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
