import { useEffect } from "react";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useCodeGraphStore } from "./codegraphStore";
import { ensureStarted } from "./codegraphActions";
import { MODULE } from "./documents";

/**
 * Whether this document is the Code Graph tab in front. Its context pane —
 * the inspector — shows only then: two flows side by side must not both
 * claim the one inspector.
 */
export function useIsFrontDocument(tabId: string): boolean {
  return useWorkbenchTabsStore((s) => moduleTabs(s.modules, MODULE).activeTabId === tabId);
}

/**
 * The project a document can load from: set, and not mid-index. A tab
 * restored after a relaunch mounts before the project is restored; asking Go
 * for its flow then would fail with "no project" and paint an error that
 * fixes itself a second later.
 */
export function useProjectReady(): boolean {
  useEffect(ensureStarted, []);
  return useCodeGraphStore((s) => Boolean(s.projectPath) && !s.busy && !s.status.startsWith("Indexing"));
}
