import { create } from "zustand";
import type { QueryResult } from "@/bridge/database";

export type SaveDraft = {
  id: number;
  profileId: string;
  profileName: string;
  engine?: string;
  sql: string;
  name: string;
  pending: boolean;
  error: string;
};
export type ExportDraft = {
  id: number;
  result: QueryResult;
  filename: string;
  format: "csv" | "json";
  pending: boolean;
  error: string;
  status: string;
};
let serial = 0;
/** Ephemeral snapshots outlive document mounting; never persist SQL/results or credentials. */
export const useDatabaseDrafts = create<{
  save: SaveDraft | null;
  export: ExportDraft | null;
  openSave: (
    profileId: string,
    sql: string,
    engine?: string,
    profileName?: string,
  ) => boolean;
  openExport: (result: QueryResult, filename: string) => boolean;
  patchSave: (id: number, patch: Partial<SaveDraft>) => void;
  patchExport: (id: number, patch: Partial<ExportDraft>) => void;
  closeSave: () => boolean;
  closeExport: () => boolean;
}>((set, get) => ({
  save: null,
  export: null,
  openSave: (profileId, sql, engine, profileName) => {
    if (get().save?.pending) return false;
    set({
      save: {
        id: ++serial,
        profileId,
        profileName: profileName || profileId,
        sql,
        engine,
        name: "",
        pending: false,
        error: "",
      },
    });
    return true;
  },
  openExport: (result, filename) => {
    if (get().export?.pending) return false;
    set({
      export: {
        id: ++serial,
        result,
        filename,
        format: "csv",
        pending: false,
        error: "",
        status: "",
      },
    });
    return true;
  },
  patchSave: (id, patch) => {
    if (get().save?.id === id) set({ save: { ...get().save!, ...patch } });
  },
  patchExport: (id, patch) => {
    if (get().export?.id === id)
      set({ export: { ...get().export!, ...patch } });
  },
  closeSave: () => {
    if (get().save?.pending) return false;
    set({ save: null });
    return true;
  },
  closeExport: () => {
    if (get().export?.pending) return false;
    set({ export: null });
    return true;
  },
}));

/** Synchronous acquisition also rejects double submits before React can paint pending. */
export async function runDraftOperation<K extends "save" | "export">(
  kind: K,
  action: (draft: K extends "save" ? SaveDraft : ExportDraft) => Promise<void>,
): Promise<boolean> {
  const state = useDatabaseDrafts.getState();
  const draft = state[kind];
  if (!draft || draft.pending) return false;
  const patch = kind === "save" ? state.patchSave : state.patchExport;
  if (kind === "export")
    state.patchExport(draft.id, { pending: true, error: "", status: "" });
  else state.patchSave(draft.id, { pending: true, error: "" });
  try {
    await action(draft as K extends "save" ? SaveDraft : ExportDraft);
  } finally {
    patch(draft.id, { pending: false });
  }
  return true;
}
