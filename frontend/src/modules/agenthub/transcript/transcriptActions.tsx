import * as React from "react";
import type { Placement } from "@/workbench/intents";
import type { FileRef } from "./fileRef";
import type { RefContext } from "./resolveFileRef";

export type TranscriptActions = {
  /** Opens an absolute location (tool-call chips); the fallback for prose when `openRef` is absent. */
  onOpenFile?: (path: string, line?: number) => void;
  /** The session prose references resolve against; null for a draft. */
  refContext?: RefContext | null;
  /** Opens a reference written in prose, resolved in the session's workspace. */
  openRef?: (ref: FileRef, anchor: HTMLElement | null, placement?: Placement) => void;
};

const Ctx = React.createContext<TranscriptActions>({});

/** What prose deep inside a turn may do, without threading props through every block. */
export function TranscriptActionsProvider({ value, children }: { value: TranscriptActions; children: React.ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTranscriptActions(): TranscriptActions {
  return React.useContext(Ctx);
}
