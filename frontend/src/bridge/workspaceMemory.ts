import { AppService } from "../../bindings/devkit/internal/bridge";
import {
  callBridge,
  callBridgeCancellable,
  type CancellableCall,
} from "./client";

export const memoryKinds = [
  "decision",
  "convention",
  "knowledge",
  "handoff",
] as const;
export type MemoryKind = (typeof memoryKinds)[number];
export type MemoryInput = {
  kind: string;
  title: string;
  summary: string;
  body: string;
};
export type MemorySource = {
  caller: string;
  sessionId: string;
  agentType: string;
};
export type MemorySummary = Omit<MemoryInput, "body"> & {
  id: string;
  workspaceId: string;
  revision: number;
  deleted: boolean;
  createdAt: string;
  updatedAt: string;
  source: MemorySource;
};
export type MemoryEntry = MemorySummary & { body: string };
export type BrowseInput = {
  query: string;
  kind: string;
  cursor: string;
  deleted: boolean;
  limit: number;
};
export type BrowsePage = {
  entries: MemorySummary[];
  hasMore: boolean;
  nextCursor: string;
};
export type RevisionSummary = {
  revision: number;
  operation: string;
  title: string;
  summary: string;
  updatedAt: string;
  deleted: boolean;
  source: MemorySource;
};
export type HistoryPage = {
  entries: RevisionSummary[];
  hasMore: boolean;
  nextCursor: string;
};
export type MemoryRevision = { entry: MemoryEntry; operation: string };

export const browseMemory = (
  ws: string,
  input: BrowseInput,
): CancellableCall<BrowsePage> =>
  callBridgeCancellable(
    () => AppService.WorkspaceMemoryBrowse(ws, input),
    (value) => ({ ...value, entries: value.entries ?? [] }),
  );
export const getMemory = (
  ws: string,
  id: string,
): CancellableCall<MemoryEntry> =>
  callBridgeCancellable(
    () => AppService.WorkspaceMemoryGet(ws, id),
    (value) => value,
  );
export const createMemory = (
  ws: string,
  input: MemoryInput,
): Promise<MemoryEntry> =>
  callBridge(() => AppService.WorkspaceMemoryCreate(ws, input));
export const updateMemory = (
  ws: string,
  id: string,
  revision: number,
  input: MemoryInput,
): Promise<MemoryEntry> =>
  callBridge(() => AppService.WorkspaceMemoryUpdate(ws, id, revision, input));
export const deleteMemory = (
  ws: string,
  id: string,
  revision: number,
): Promise<MemoryEntry> =>
  callBridge(() => AppService.WorkspaceMemoryDelete(ws, id, revision));
export const restoreMemory = (
  ws: string,
  id: string,
  revision: number,
  target: number,
): Promise<MemoryEntry> =>
  callBridge(() => AppService.WorkspaceMemoryRestore(ws, id, revision, target));
export const memoryHistory = (
  ws: string,
  id: string,
  cursor: string,
): CancellableCall<HistoryPage> =>
  callBridgeCancellable(
    () => AppService.WorkspaceMemoryHistoryPage(ws, id, cursor, 20),
    (value) => ({ ...value, entries: value.entries ?? [] }),
  );
export const readMemoryRevision = (
  ws: string,
  id: string,
  revision: number,
): CancellableCall<MemoryRevision> =>
  callBridgeCancellable(
    () => AppService.WorkspaceMemoryReadRevision(ws, id, revision),
    (value) => value,
  );
