import { create } from "zustand";

/**
 * A note from another module — "this repository needs a GitLab token" — that
 * Accounts reads when it is on screen: it opens the add-token form with that
 * provider chosen, and, when the note is about an SSH key, with the token to be
 * attached to that key. The request is a provider id and an account id, nothing
 * else, and it is consumed once, so reopening Accounts later does not pop the
 * form again.
 */
export type AddTokenWish = { provider: string; keyId: string };

type AddTokenRequest = {
  provider: string | null;
  keyId: string;
  request: (provider: string, keyId?: string) => void;
  take: () => AddTokenWish | null;
};

export const useAddTokenRequest = create<AddTokenRequest>((set, get) => ({
  provider: null,
  keyId: "",
  request: (provider, keyId = "") => set({ provider, keyId }),
  take: () => {
    const { provider, keyId } = get();
    if (provider === null) return null;
    set({ provider: null, keyId: "" });
    return { provider, keyId };
  },
}));
