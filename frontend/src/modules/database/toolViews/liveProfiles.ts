import { requestConfirm } from "@/workbench/confirmRequest";
import { useDatabaseStore } from "../databaseStore";
import type { ProfileSelection } from "./reveal";

/** The live store behind `selectProfileById`: shared by database.reveal and Open in Database. */
export const liveProfileSelection: ProfileSelection = {
  profileIds: () => useDatabaseStore.getState().profiles.map((profile) => profile.id),
  selectedProfileId: () => useDatabaseStore.getState().selectedProfileId,
  // No auto-select: with nothing selected the list's first connection would be
  // picked (and connected to) before the agent's one is.
  refreshProfiles: () => useDatabaseStore.getState().fetchProfiles({ autoSelect: false }),
  selectProfile: (id) => useDatabaseStore.getState().setSelectedProfileId(id),
  // What setSelectedProfileId throws away: the old session (and with it an
  // open transaction) and every console run in flight.
  switchCost: (id) => {
    const state = useDatabaseStore.getState();
    if (!state.selectedProfileId || state.selectedProfileId === id) return null;
    const runningQueries = Object.values(state.consoles).filter((console) => console.runningQuery).length;
    if (!state.txOpen && runningQueries === 0) return null;
    const name = (profileId: string) => state.profiles.find((profile) => profile.id === profileId)?.name ?? "another connection";
    return { from: name(state.selectedProfileId), to: name(id), transaction: state.txOpen, runningQueries };
  },
  confirm: (message) =>
    requestConfirm({ title: "Switch connection?", description: message, confirmLabel: "Switch" }),
};
