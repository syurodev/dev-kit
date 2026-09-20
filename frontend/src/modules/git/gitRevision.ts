import { create } from "zustand";

/**
 * A counter the Git views bump when they change the repository, so the rail
 * beside them reads it again. Git has no event for "the work tree changed", and
 * the views are the only thing in the app that changes it: staging, unstaging
 * and committing all go through GitChangesView, which bumps after each.
 */
export const useGitRevision = create<{ revision: number }>()(() => ({ revision: 0 }));

export function bumpGitRevision() {
  useGitRevision.setState((state) => ({ revision: state.revision + 1 }));
}
