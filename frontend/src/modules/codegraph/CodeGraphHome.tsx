import { useMemo } from "react";
import { FolderGit2, GitBranch, RotateCw, Workflow } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Chip } from "@/components/ui/chip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import { modKey } from "@/lib/shortcut";
import { AnalyzerSetup } from "./AnalyzerSetup";
import { useCodeGraphStore } from "./codegraphStore";
import { loadAnalyzerStatus, reindexFolders, retryFailed, setScope, viewFlow } from "./codegraphActions";
import { set, snapshot } from "./storeAccess";
import { useStarted } from "./useDocument";
import { DOT, PrimaryBadge, useFolderHealths } from "./sidebar/WorkspaceHeader";
import { IndexErrorCard } from "./sidebar/IndexErrorCard";
import { askAgent } from "./codegraphActions";
import { folderLabels } from "./workspace/identity";
import { scopeFolders, scopeSummary } from "./workspace/scope";

/** One key and what it does, in the home view's list of ways in. */
function Hint({ keys, children }: { keys: string; children: string }) {
  return (
    <li className="flex items-center justify-between gap-6">
      <span className="text-body-12 text-text-secondary">{children}</span>
      <kbd className="rounded-sm bg-state-soft px-1.5 py-0.5 font-sans text-code-11 text-text-tertiary">{keys}</kbd>
    </li>
  );
}

const openFiles = (folderId: string) => {
  const s = snapshot();
  // Out of the sidebar's view: bring it in, as the only thing in view would hide the rest.
  if (!scopeFolders(s.scope, s.folders).some((f) => f.id === folderId)) setScope({ kind: "folders", ids: [folderId] });
  set({ activeTab: "files", collapsedRoots: snapshot().collapsedRoots.filter((id) => id !== folderId) });
};
const openEntries = (folderId: string) => {
  const s = snapshot();
  if (!scopeFolders(s.scope, s.folders).some((f) => f.id === folderId)) setScope({ kind: "folders", ids: [folderId] });
  set({ activeTab: "entries" });
};

/**
 * The module's home tab: the workspace at a glance when nothing is open. Each
 * folder with what was recognised in it (language, services, entry points),
 * how its index stands, and the ways in — its files, its entry points, a flow.
 * Counts are per folder and carry the age of the index they come from; none is
 * summed into a figure that would pretend every folder is as current as the
 * freshest. Flows, files and the plan are tabs of their own (documents.ts).
 */
export function CodeGraphHome() {
  useStarted();
  const workspaceName = useCodeGraphStore((s) => s.workspace?.name ?? "");
  const status = useCodeGraphStore((s) => s.foldersStatus);
  const error = useCodeGraphStore((s) => s.foldersError);
  const folders = useCodeGraphStore((s) => s.folders);
  const scope = useCodeGraphStore((s) => s.scope);
  const primaryId = useCodeGraphStore((s) => s.primaryId);
  const data = useCodeGraphStore((s) => s.data);
  const analyzerStatuses = useCodeGraphStore((s) => s.analyzerStatuses);
  const workspaceId = useCodeGraphStore((s) => s.workspaceId);
  const healths = useFolderHealths();
  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const healthOf = new Map(healths.map((h) => [h.id, h.health]));
  const inView = scopeFolders(scope, folders);

  if (!workspaceId) {
    return <EmptyState icon={FolderGit2} title="No workspace selected" body="Pick or create a workspace from the switcher in the title bar. Code Graph reads every folder of it." />;
  }
  if (status === "error") return <EmptyState tone="danger" title="Could not read the workspace" body={error} />;
  if (status !== "ready") return <EmptyState busy title="Reading the workspace" body="Listing its folders…" />;
  if (folders.length === 0) {
    return (
      <EmptyState
        icon={FolderGit2}
        title="This workspace has no folders yet"
        body="Add folders from Edit workspace in the title bar's workspace menu. Code Graph indexes each one and shows what it finds here."
      />
    );
  }

  const needsAnalyzer = inView.some((f) => healthOf.get(f.id)?.health === "needs-analyzer");
  const missing = (analyzerStatuses ?? []).filter((s) => !s.ready);
  const failed = healths.filter((h) => h.health.health === "failed").length;

  return (
    <div className="flex h-full w-full flex-col overflow-auto">
      <div className="mx-auto flex w-full max-w-180 flex-col gap-4 p-8">
        <header className="flex flex-col gap-1">
          <h2 className="text-title-16 text-text-primary">{workspaceName || "Workspace"}</h2>
          <p className="text-body-12 text-text-tertiary">
            {scopeSummary(scope, folders, labels)}
            {inView.length < folders.length ? ` — ${folders.length - inView.length} more outside the current view` : ""}
          </p>
        </header>

        <ul className="flex flex-col gap-2" aria-label="Folders">
          {inView.map((f) => {
            const d = data[f.id];
            const health = healthOf.get(f.id);
            const entries = d?.entries;
            const services = [...new Set((entries?.entries ?? []).map((e) => e.service).filter(Boolean))];
            const http = (entries?.entries ?? []).filter((e) => e.kind === "http").slice(0, 8);
            return (
              <li key={f.id} className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-surface-raised p-3">
                <div className="flex items-start gap-2">
                  <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", DOT[health?.tone ?? "neutral"])} aria-hidden />
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="truncate text-title-13 text-text-primary">{labels[f.id]}</span>
                      {f.id === primaryId && folders.length > 1 ? <PrimaryBadge /> : null}
                      {d?.state?.isRepo && d.state.branch ? (
                        <Chip>
                          <GitBranch className="size-3" aria-hidden />
                          {d.state.branch}
                        </Chip>
                      ) : null}
                    </div>
                    <span className="truncate text-code-11 text-text-tertiary" title={f.path}>{f.path}</span>
                    <span className="text-body-12 text-text-secondary">
                      {health?.label ?? "Checking"}
                      {entries?.state === "indexed" ? ` · ${entries.entries.length} entry point${entries.entries.length === 1 ? "" : "s"}` : ""}
                      {health?.detail ? ` — ${health.detail}` : ""}
                    </span>
                    {services.length > 0 ? (
                      <span className="flex flex-wrap items-center gap-1 pt-1">
                        {services.slice(0, 6).map((svc) => (
                          <Chip key={svc}>{svc}</Chip>
                        ))}
                        {services.length > 6 ? <span className="text-code-11 text-text-tertiary">+{services.length - 6}</span> : null}
                      </span>
                    ) : null}
                  </div>
                </div>
                {health?.health === "failed" && (health.detail || d?.indexStatus?.error) ? (
                  <IndexErrorCard error={d?.indexStatus?.error || health.detail} busy={false} onRetry={() => void reindexFolders([f.id])} onAskAgent={askAgent} />
                ) : null}
                <div className="flex flex-wrap items-center gap-1.5">
                  <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => openFiles(f.id)}>
                    Files
                  </Button>
                  <Button size="xs" variant="outline" className="bg-surface-raised" disabled={entries?.state === "unsupported"} onClick={() => openEntries(f.id)}>
                    Entry points
                  </Button>
                  {http.length > 0 ? (
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button size="xs" variant="outline" className="bg-surface-raised">
                          <Workflow aria-hidden /> Open a flow
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="start" className="w-72">
                        <DropdownMenuLabel>{labels[f.id]}</DropdownMenuLabel>
                        {http.map((ep) => (
                          <DropdownMenuItem key={ep.id} onSelect={() => viewFlow({ workspaceId, folderId: f.id }, ep, { preview: false })}>
                            <span className="truncate">{ep.verb} {ep.target}</span>
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  ) : null}
                  <Button size="xs" variant="ghost" onClick={() => void reindexFolders([f.id])} disabled={health?.health === "indexing" || health?.health === "pending"}>
                    <RotateCw aria-hidden /> Reindex
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>

        {failed > 1 ? (
          <div>
            <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => void retryFailed()}>
              Retry {failed} failed folders
            </Button>
          </div>
        ) : null}

        {needsAnalyzer && missing.length > 0 ? (
          <AnalyzerSetup
            statuses={missing}
            onBuilt={() => {
              void loadAnalyzerStatus();
              // Folders that waited for the analyzer can be indexed now.
              void retryFailed();
            }}
          />
        ) : null}

        <ul className="flex w-64 flex-col gap-2 self-center">
          <Hint keys="Click">Preview in this tab</Hint>
          <Hint keys="Double-click">Keep it open</Hint>
          <Hint keys={`${modKey()}-click`}>Open beside</Hint>
          <Hint keys="@">Search classes and methods</Hint>
        </ul>
      </div>
    </div>
  );
}
