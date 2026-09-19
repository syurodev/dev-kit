import { useMemo } from "react";
import { FolderSearch, GitFork, Loader2, RotateCcw, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  CraftButton,
  CraftButtonIcon,
  CraftButtonLabel,
} from "@/components/ui/craft-button";
import { EmptyProgress, EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import { FlowCanvas, describeCounts, type CanvasCounts } from "./graph/FlowCanvas";
import { methodChipClass } from "./graph/tokens";
import { useCodeGraphStore } from "./codegraphStore";
import {
  inspectNode,
  loadMoreNeighbors,
  pickAndOpenProject,
  reindex,
} from "./codegraphActions";

const onSelectNode = (nodeId: string) => void inspectNode(nodeId);

/** The Endpoints tab: one endpoint's call graph, or why there is none yet. */
export function EndpointsPane() {
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const endpoints = useCodeGraphStore((s) => s.endpoints);
  const subgraph = useCodeGraphStore((s) => s.subgraph);
  const rootId = useCodeGraphStore((s) => s.rootId);
  const rootEndpointId = useCodeGraphStore((s) => s.rootEndpointId);
  const error = useCodeGraphStore((s) => s.error);
  const errorCode = useCodeGraphStore((s) => s.errorCode);
  const busy = useCodeGraphStore((s) => s.busy);
  const loadingMore = useCodeGraphStore((s) => s.loadingMore);
  const rootEndpoint = useMemo(
    () =>
      endpoints.find((ep) => ep.id === rootEndpointId) ??
      endpoints.find((ep) => ep.entryNode === rootId) ??
      null,
    [endpoints, rootEndpointId, rootId],
  );

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {subgraph && rootId ? (
        <FlowCanvas
          key={rootId}
          rootId={rootId}
          nodes={subgraph.nodes}
          edges={subgraph.edges}
          onSelect={onSelectNode}
          header={
            rootEndpoint ? (
              <>
                <span
                  className={cn(
                    "rounded-sm px-2 py-0.75 text-code-12-medium uppercase",
                    methodChipClass(rootEndpoint.httpMethod),
                  )}
                >
                  {rootEndpoint.httpMethod}
                </span>
                <span className="truncate text-title-16 text-text-primary">{rootEndpoint.path}</span>
              </>
            ) : (
              <span className="truncate text-title-16 text-text-primary">{rootId}</span>
            )
          }
          meta={(counts: CanvasCounts) => (
            <>
              <span className="text-code-11 text-text-tertiary">{describeCounts(counts)}</span>
              {subgraph.truncated && (
                <>
                  <span
                    title="The backend capped this subgraph; some nodes are not shown yet."
                    className="text-code-11 text-status-warning"
                  >
                    partial
                  </span>
                  <Button
                    variant="outline"
                    size="xs"
                    className="bg-surface-raised"
                    disabled={loadingMore || !subgraph.continuation}
                    onClick={() => void loadMoreNeighbors()}
                  >
                    {loadingMore ? "Loading…" : "Load more"}
                  </Button>
                </>
              )}
            </>
          )}
          hint={
            <>
              <strong className="text-text-primary">Click</strong> to open code
            </>
          }
        />
      ) : busy ? (
        <EmptyState
          icon={Loader2}
          title="Analyzing codebase"
          body="Indexing controllers, services and repositories. The graph opens as soon as the first route resolves."
        >
          <EmptyProgress />
        </EmptyState>
      ) : error && errorCode === "unsupported_project" ? (
        /* Nothing was analyzed and nothing is wrong with the analyzer —
           the folder simply is not a project. Offering a retry here would
           be a button that cannot work; the only useful move is choosing
           somewhere else. */
        <EmptyState icon={FolderSearch} title="Nothing to index in that folder" body={error}>
          <Button onClick={pickAndOpenProject}>Choose another folder…</Button>
        </EmptyState>
      ) : error ? (
        <EmptyState icon={TriangleAlert} title="Could not analyze this project" body={error}>
          <Button
            variant="outline"
            className="bg-surface-raised"
            onClick={() => reindex(projectPath)}
            disabled={!projectPath}
          >
            Re-run analysis
            <RotateCcw className="size-4" />
          </Button>
        </EmptyState>
      ) : (
        <EmptyState
          icon={GitFork}
          title={projectPath ? "Pick an endpoint to graph" : "Open a Java project"}
          body={
            projectPath
              ? "Choose a route on the left and DevKit traces its controller, services and repositories from the analyzed codebase."
              : "DevKit indexes the project's controllers, services and repositories, then draws the call chain behind each route."
          }
        >
          {projectPath ? (
            <span className="flex items-center gap-2 rounded-md bg-state-soft px-2.5 py-1.25 text-body-12 text-text-tertiary">
              <span className="text-code-11">⌘K</span>
              search endpoints
            </span>
          ) : (
            /* Alone on the screen and an invitation, so it takes the
               craft button — unlike the two buttons further up, which
               recover from an error and should not be celebrating. */
            <CraftButton onClick={pickAndOpenProject}>
              <CraftButtonIcon>
                <FolderSearch className="size-3" />
              </CraftButtonIcon>
              <CraftButtonLabel>Open Java project…</CraftButtonLabel>
            </CraftButton>
          )}
        </EmptyState>
      )}
    </div>
  );
}
