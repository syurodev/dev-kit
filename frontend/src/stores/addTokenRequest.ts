import { create } from "zustand";

/**
 * A note from another module — "this repository needs a GitLab token" — that
 * Accounts reads when it is on screen: it opens the add-token form with that
 * provider chosen. The request is a provider id and nothing else, and it is
 * consumed once, so reopening Accounts later does not pop the form again.
 */
type AddTokenRequest = {
  provider: string | null;
  request: (provider: string) => void;
  take: () => string | null;
};

export const useAddTokenRequest = create<AddTokenRequest>((set, get) => ({
  provider: null,
  request: (provider) => set({ provider }),
  take: () => {
    const { provider } = get();
    if (provider !== null) set({ provider: null });
    return provider;
  },
}));
