import { create } from "zustand";
import { deleteSavedQuery } from "@/bridge/database";
import { errorMessage } from "@/bridge/errors";
import type { LibraryEntry } from "./queryLibrary";
type Intent = {
  profileId: string;
  entry: LibraryEntry;
  pending: boolean;
  error: string;
};
/** A delete continues across document mounting; failure stays with its captured target. */
export const useSavedQueryDeletion = create<{
  intent: Intent | null;
  open: (profileId: string, entry: LibraryEntry) => void;
  close: () => void;
}>((set, get) => ({
  intent: null,
  open: (profileId, entry) => {
    if (!get().intent?.pending)
      set({ intent: { profileId, entry, pending: false, error: "" } });
  },
  close: () => {
    if (!get().intent?.pending) set({ intent: null });
  },
}));
export async function confirmSavedQueryDeletion(
  invalidate: (profileId: string) => void,
  remove = deleteSavedQuery,
) {
  const target = useSavedQueryDeletion.getState().intent;
  if (!target || target.pending) return;
  useSavedQueryDeletion.setState({
    intent: { ...target, pending: true, error: "" },
  });
  try {
    await remove(target.entry.id);
    invalidate(target.profileId);
    useSavedQueryDeletion.setState({ intent: null });
  } catch (err) {
    useSavedQueryDeletion.setState({
      intent: { ...target, pending: false, error: errorMessage(err) },
    });
  }
}
