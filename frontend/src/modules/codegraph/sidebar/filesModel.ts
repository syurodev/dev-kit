import type { CodeGraphFileEntry } from "@/bridge/codegraph";
import { filterFileEntries } from "../fileSearch";
import { compactFiles, nestFiles, type FileBranch } from "./treeNodes";

/**
 * The Files tab's shaping, pure: what each folder of the workspace shows under
 * the toggles and the search, and how many of its files that hides.
 */

/**
 * Source code in any language a project is likely to mix — a folder of
 * microservices is Java beside Go beside TypeScript, and the analyzer will
 * read more than Java. What it hides is what is not code: build files,
 * config, resources, lockfiles, docs.
 */
export const CODE =
  /\.(java|kt|kts|groovy|scala|go|py|ts|tsx|js|jsx|mjs|cjs|rs|rb|php|cs|fs|swift|m|mm|c|h|cc|cpp|hpp|dart|ex|exs|erl|clj|lua|sql|proto|graphql|vue|svelte|sh|tf|tfvars|hcl|bicep|nix|rego|cue|jsonnet|libsonnet)$/i;

export type FileFilter = { codeOnly: boolean; changed: boolean; inFlow: boolean };

/** The files of the flow in front, and the folder they are in. */
export type FlowFiles = { folderId: string; files: ReadonlySet<string> } | null;

export type FolderView = {
  folderId: string;
  branches: FileBranch[];
  /** Folders a search forces open so its matches can be seen. */
  forceExpanded: Set<string> | null;
  /** Files (not directories) listed, and how many the folder has. */
  shown: number;
  total: number;
  /** Which toggles are responsible for files being hidden. */
  hiddenBy: Array<"code" | "changed" | "flow">;
};

export const countFiles = (entries: readonly CodeGraphFileEntry[]) => entries.reduce((n, e) => n + (e.isDir ? 0 : 1), 0);

/**
 * One folder's tree under the filters. A folder an analyzer cannot read has no
 * "code" — in a Helm chart the YAML is the code — so the Code toggle does not
 * apply to it, and a toggle that would hide a whole folder says so through
 * `hiddenBy`.
 */
export function folderView(
  folderId: string,
  files: readonly CodeGraphFileEntry[],
  query: string,
  filter: FileFilter,
  opts: { unsupported: boolean; git: Readonly<Record<string, string>>; flow: FlowFiles },
): FolderView {
  const codeOnly = filter.codeOnly && !opts.unsupported;
  const inFlow = filter.inFlow;
  const flowHere = opts.flow && opts.flow.folderId === folderId ? opts.flow.files : null;
  const keep =
    codeOnly || filter.changed || inFlow
      ? (path: string) =>
          (!codeOnly || CODE.test(path)) &&
          (!filter.changed || path in opts.git) &&
          // The flow in front belongs to one folder; in the others nothing is "in" it.
          (!inFlow || (flowHere !== null && flowHere.has(path)))
      : undefined;
  const result = filterFileEntries(files as CodeGraphFileEntry[], query, keep);
  const total = countFiles(files);
  const shown = countFiles(result.entries);
  const hiddenBy: FolderView["hiddenBy"] = [];
  if (shown < total && !query.trim()) {
    if (codeOnly) hiddenBy.push("code");
    if (filter.changed) hiddenBy.push("changed");
    if (inFlow) hiddenBy.push("flow");
  }
  return {
    folderId,
    branches: compactFiles(nestFiles(result.entries)),
    forceExpanded: result.forceExpanded,
    shown,
    total,
    hiddenBy,
  };
}

/** A sentence for the notice a hiding toggle owes the person. */
export function hiddenNotice(view: Pick<FolderView, "shown" | "total" | "hiddenBy">): string {
  const by = view.hiddenBy.map((h) => (h === "code" ? "Code only" : h === "changed" ? "Changed" : "In flow"));
  const hidden = view.total - view.shown;
  if (hidden <= 0 || by.length === 0) return "";
  return `${hidden} file${hidden === 1 ? "" : "s"} hidden by ${by.join(" + ")}`;
}

/** Whether any toggle is on, so "Reset filters" has something to reset. */
export const filtersActive = (f: FileFilter, defaults: FileFilter = { codeOnly: true, changed: false, inFlow: false }) =>
  f.codeOnly !== defaults.codeOnly || f.changed !== defaults.changed || f.inFlow !== defaults.inFlow;
