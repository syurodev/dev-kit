import * as React from "react";

export type TranscriptActions = { onOpenFile?: (path: string, line?: number) => void };

const Ctx = React.createContext<TranscriptActions>({});

/** What prose deep inside a turn may do, without threading props through every block. */
export function TranscriptActionsProvider({ value, children }: { value: TranscriptActions; children: React.ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTranscriptActions(): TranscriptActions {
  return React.useContext(Ctx);
}
