import { expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import WorkspaceMemoryDialog from "./WorkspaceMemoryDialog";

test("memory manager initially names its pinned workspace and exposes accessible bounded browse controls", () => {
  const html = renderToString(
    <WorkspaceMemoryDialog
      workspace={{ id: "ws-original", name: "Original workspace" }}
      onClose={() => {}}
      portal={false}
    />,
  );
  expect(html).toContain("Original workspace");
  expect(html).toContain('aria-label="Memory kind filter"');
  expect(html).toContain('aria-label="Memory status filter"');
  expect(html).toContain('id="workspace-memory-search"');
  expect(html).toContain("Search words in titles, summaries and details.");
  expect(html).toContain("New memory");
  expect(html).toContain("Previous");
  expect(html).toContain("Next");
  expect(html).toContain("Loading memory");
  expect(html).toContain("Select a memory to read or edit it.");
});
