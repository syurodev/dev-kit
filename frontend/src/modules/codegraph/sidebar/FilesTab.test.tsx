import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { TooltipProvider } from "@/components/ui/tooltip";
import type { WorkspaceService } from "@/bridge/workspace";
import { emptyFolderData, type FolderData } from "../codegraphStore";
import { FilesTab, type FilesTabActions } from "./FilesTab";
import { treeDirId } from "../workspace/identity";

const folder = (id: string, name: string): WorkspaceService => ({ id, workspace_id: "w", name, path: `/ws/${name}`, name_source: "" });

const files = [
  { path: "charts", isDir: true },
  { path: "charts/values.yaml", isDir: false },
  { path: "main.tf", isDir: false },
];

const noop = () => {};
const actions: FilesTabActions = {
  onFilter: noop,
  onToggleFolder: noop,
  onToggleRoot: noop,
  onSelectFile: noop,
  onKeepFile: noop,
  onOpenBeside: noop,
  onFindInFolder: noop,
  onShowOnly: noop,
  onReindexFolder: noop,
  onMakePrimary: noop,
  onCopyPath: noop,
  onResetFilters: noop,
};

const data = (state: "unsupported" | "indexed", list = files): FolderData => ({
  ...emptyFolderData(),
  files: list,
  filesStatus: "ready",
  entries: { state, entries: [], indexedAt: 0, stale: false, error: "", missingToolchain: false },
});

const render = (folders: WorkspaceService[], byFolder: Record<string, FolderData>, expandedDirs: string[] = []) =>
  renderToString(
    <TooltipProvider>
      <FilesTab
        folders={folders}
        labels={Object.fromEntries(folders.map((f) => [f.id, f.name]))}
        primaryId={folders[0].id}
        data={byFolder}
        query=""
        selected={null}
        unsaved={new Set()}
        workspaceId="w"
        flow={null}
        filter={{ codeOnly: true, changed: false, inFlow: false }}
        expanded={expandedDirs}
        collapsedRoots={[]}
        actions={actions}
      />
    </TooltipProvider>,
  );

describe("FilesTab", () => {
  it("shows every file of a folder no analyzer reads, whatever the code filter says", () => {
    // In a Helm chart the YAML is the code; the filter, on by default, hid it.
    const a = folder("fa", "chart");
    const html = render([a], { fa: data("unsupported") }, [treeDirId("fa", "charts")]);
    expect(html).toContain("values.yaml");
    expect(html).toContain("main.tf");
    expect(html).not.toContain(">CODE<");
  });

  it("keeps the code filter for a folder an analyzer reads", () => {
    const a = folder("fa", "svc");
    const html = render([a], { fa: data("indexed") }, [treeDirId("fa", "charts")]);
    expect(html).toContain(">CODE<");
    expect(html).toContain("main.tf");
    expect(html).not.toContain("values.yaml");
  });

  it("draws one root per folder when several are in view, each with its own files", () => {
    const a = folder("fa", "orders");
    const b = folder("fb", "billing");
    const html = render([a, b], {
      fa: data("indexed", [{ path: "src/a.ts", isDir: false }]),
      fb: data("indexed", [{ path: "src/a.ts", isDir: false }]),
    });
    expect(html).toContain("orders");
    expect(html).toContain("billing");
    // The same relative path is a row under each root, not one shared row.
    expect(html.split("a.ts").length - 1).toBeGreaterThanOrEqual(2);
  });

  it("says a folder is unavailable instead of drawing it empty", () => {
    const a = folder("fa", "gone");
    const d = { ...data("indexed", []), state: { folderId: "fa", accessible: false, error: "folder missing", isRepo: false, branch: "", changed: 0 } };
    const html = render([a, folder("fb", "other")], { fa: d, fb: data("indexed") });
    expect(html).toContain("folder missing");
  });
});
