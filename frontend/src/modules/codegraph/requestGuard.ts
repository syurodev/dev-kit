/**
 * Drops answers to questions nobody is asking any more.
 *
 * Every CodeGraph action awaits the bridge, and the user does not wait for it:
 * they click a second endpoint before the first one's neighbours arrive, or the
 * project changes under an inspector that is still fetching a snippet. Without
 * a guard, whichever response lands LAST wins, not whichever was asked last —
 * the canvas shows endpoint A under endpoint B's header.
 *
 * begin(key) supersedes every earlier request under the same key and returns a
 * check the caller runs after each await. invalidateAll() supersedes every
 * request under every key at once — what a project switch means, since nothing
 * asked of the old project is an answer about the new one.
 */
export type RequestGuard = {
  begin: (key: string) => () => boolean;
  invalidateAll: () => void;
};

export function createRequestGuard(): RequestGuard {
  const counters = new Map<string, number>();
  let epoch = 0;
  return {
    begin(key) {
      const n = (counters.get(key) ?? 0) + 1;
      counters.set(key, n);
      const startedIn = epoch;
      return () => counters.get(key) === n && epoch === startedIn;
    },
    invalidateAll() {
      epoch += 1;
    },
  };
}
