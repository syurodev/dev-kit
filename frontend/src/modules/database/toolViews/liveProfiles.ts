import { useDatabaseStore } from "../databaseStore";
import type { ProfileSelection } from "./reveal";

/** The live store behind `selectProfileById`: shared by database.reveal and Open in Database. */
export const liveProfileSelection: ProfileSelection = {
  profileIds: () => useDatabaseStore.getState().profiles.map((profile) => profile.id),
  selectedProfileId: () => useDatabaseStore.getState().selectedProfileId,
  refreshProfiles: () => useDatabaseStore.getState().fetchProfiles(),
  selectProfile: (id) => useDatabaseStore.getState().setSelectedProfileId(id),
};
