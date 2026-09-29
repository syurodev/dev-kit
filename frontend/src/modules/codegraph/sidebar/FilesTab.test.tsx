import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { TooltipProvider } from "@/components/ui/tooltip";
import { FilesTab } from "./FilesTab";

const files = [
  { path: "charts", isDir: true },
  { path: "charts/values.yaml", isDir: false },
  { path: "main.tf", isDir: false },
];

const render = (unsupported: boolean) =>
  renderToString(
    <TooltipProvider>
      <FilesTab
        files={files}
        query=""
        selectedFile={null}
        unsaved={new Set()}
        gitStatus={{}}
        flowFiles={new Set()}
        filter={{ codeOnly: true, changed: false, inFlow: false }}
        onFilter={() => {}}
        unsupported={unsupported}
        expanded={["charts"]}
        onToggleFolder={() => {}}
        onSelectFile={() => {}}
        onKeepFile={() => {}}
      />
    </TooltipProvider>,
  );

describe("FilesTab", () => {
  it("shows every file of a project no analyzer reads, whatever the code filter says", () => {
    // In a Helm chart the YAML is the code; the filter, on by default, hid it.
    const html = render(true);
    expect(html).toContain("values.yaml");
    expect(html).toContain("main.tf");
    expect(html).not.toContain(">CODE<");
  });

  it("keeps the code filter for a project an analyzer reads", () => {
    const html = render(false);
    expect(html).toContain(">CODE<");
    expect(html).toContain("main.tf");
    expect(html).not.toContain("values.yaml");
  });
});
