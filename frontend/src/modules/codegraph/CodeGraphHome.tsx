import { FolderOpen, FolderSearch, GitFork, RotateCcw, TriangleAlert } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import { modKey } from "@/lib/shortcut";
import { AnalyzerSetup } from "./AnalyzerSetup";
import { blockingAnalyzers } from "./analyzerGate";
import { useCodeGraphStore } from "./codegraphStore";
import { loadAnalyzerStatus, pickAndOpenProject, reindex } from "./codegraphActions";
import { useProjectReady } from "./useDocument";
import { NOT_SUPPORTED } from "./sidebar/sidebarModel";

const snapshot = () => useCodeGraphStore.getState();

const onAnalyzerBuilt = () => {
  void loadAnalyzerStatus();
  // The project the user already chose is still selected; index it now
  // rather than making them pick it again.
  const path = snapshot().projectPath;
  if (path) void reindex(path);
};

/** One key and what it does, in the home view's list of ways in. */
function Hint({ keys, children }: { keys: string; children: string }) {
  return (
    <li className="flex items-center justify-between gap-6">
      <span className="text-body-12 text-text-secondary">{children}</span>
      <kbd className="rounded-sm bg-state-soft px-1.5 py-0.5 font-sans text-code-11 text-text-tertiary">{keys}</kbd>
    </li>
  );
}

/**
 * The module's home tab: what the project is doing when nothing is open —
 * analyzing, broken, not chosen yet — and how to open something when it is
 * ready. Flows, files and the plan are tabs of their own (documents.ts).
 */
export function CodeGraphHome() {
  useProjectReady();
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const busy = useCodeGraphStore((s) => s.busy || s.status.startsWith("Indexing"));
  const error = useCodeGraphStore((s) => s.error);
  const errorCode = useCodeGraphStore((s) => s.errorCode);
  const entryCount = useCodeGraphStore((s) => s.entryPoints.length);
  const unsupported = useCodeGraphStore((s) => s.indexInfo?.unsupported ?? false);
  const analyzerStatuses = useCodeGraphStore((s) => s.analyzerStatuses);
  // What the open project turned out to be written in, "" until the first
  // index answers (and for a project whose open failed before detection).
  const projectLanguage = useCodeGraphStore((s) => s.indexInfo?.language ?? "");
  const missingAnalyzers = blockingAnalyzers(analyzerStatuses, projectPath, projectLanguage);

  // Nothing in CodeGraph works without an analyzer, so setup takes the whole
  // canvas. The sidebar is still there, so the project picker stays reachable.
  if (missingAnalyzers.length > 0) {
    return <AnalyzerSetup statuses={missingAnalyzers} onBuilt={onAnalyzerBuilt} />;
  }

  if (busy) {
    return (
      <EmptyState
        busy
        title="Analyzing codebase"
        body="Indexing controllers, services and repositories. Entry points appear in the sidebar when it finishes."
      />
    );
  }

  if (projectPath && (unsupported || errorCode === "unsupported_project")) {
    // Not an error, so none of an error's clothes: no danger tone, no retry
    // (it could never work) and no "choose another folder" (this one is
    // fine). A Terraform or Helm repository is a project; CodeGraph just has
    // no graph of it. What it does have is in the sidebar.
    return (
      <EmptyState
        icon={FolderOpen}
        title={NOT_SUPPORTED}
        body="CodeGraph has no analyzer for this project's language, so there is no graph to draw. Its files are in the sidebar — open one to read or edit it."
      >
        <ul className="flex w-64 flex-col gap-2">
          <Hint keys="Click">Preview a file in this tab</Hint>
          <Hint keys="Double-click">Keep it open</Hint>
          <Hint keys={`${modKey()}-click`}>Open beside</Hint>
        </ul>
      </EmptyState>
    );
  }

  if (error) {
    return (
      <EmptyState
        icon={TriangleAlert}
        tone="danger"
        title="Could not analyze this project"
        body={error}
        action={{ label: "Re-run analysis", icon: RotateCcw, onClick: () => void reindex(projectPath), disabled: !projectPath }}
      />
    );
  }

  if (!projectPath) {
    return (
      <EmptyState
        icon={GitFork}
        title="Open a project"
        body="DevKit indexes a Java, Lua or TypeScript project's controllers, services and repositories, then draws the call chain behind each route."
        // Alone on the screen and an invitation, so EmptyState gives it the
        // craft button — unlike the recovery button above.
        action={{ label: "Open project…", icon: FolderSearch, onClick: () => void pickAndOpenProject() }}
      />
    );
  }

  return (
    <EmptyState
      icon={GitFork}
      title={entryCount > 0 ? "Pick an entry point to graph" : "No entry points in this project"}
      body={
        entryCount > 0
          ? "Each entry point, file and the plan opens as a tab of its own — keep a flow beside the code it passes through."
          : "Type @ in the sidebar's search to graph any class or method instead."
      }
    >
      <ul className="flex w-64 flex-col gap-2">
        <Hint keys="Click">Preview in this tab</Hint>
        <Hint keys="Double-click">Keep it open</Hint>
        <Hint keys={`${modKey()}-click`}>Open beside</Hint>
        <Hint keys="@">Search classes and methods</Hint>
      </ul>
    </EmptyState>
  );
}
