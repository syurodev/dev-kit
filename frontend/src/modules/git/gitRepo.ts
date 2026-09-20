import { create } from "zustand";
import {
  gitGuards,
  gitLog,
  gitRemotes,
  gitRepositories,
  gitStatus,
  type Commit,
  type GitStatus,
  type Guard,
  type Remote,
  type Repository,
} from "@/bridge/git";

/**
 * What Git has read, in one place. The rail, the Changes view and the folder
 * list all show the same repository, and each used to ask Go for it itself:
 * twice the status on every change, twice the scan of a folder of repositories
 * on every folder change, and a view that remounted started from nothing.
 *
 * Keyed by the folder Git was in when it asked, and kept when the folder moves
 * on: coming back to a repository paints what it last showed at once and the
 * fresh read replaces it (docs/dev-kit/smoothness.mdx 2.3, 2.4).
 */

/** How many commits the rail lists; the Changes view does not use them. */
export const COMMIT_ROWS = 20;
/** Repositories remembered before the oldest is let go: enough for a large monorepo folder. */
const MAX_FOLDERS = 50;

export type RepoSnapshot = { status: GitStatus; commits: Commit[]; guards: Guard[]; remotes: Remote[] };
/** `snapshot` is what was last read; `error` is why the latest read failed, and does not clear the snapshot. */
export type RepoEntry = { snapshot: RepoSnapshot | null; error: string };
export type RepositoriesEntry = { repos: Repository[]; error: string };

export type GitReads = {
  status: () => Promise<GitStatus>;
  log: (limit: number) => Promise<Commit[]>;
  guards: () => Promise<Guard[]>;
  remotes: () => Promise<Remote[]>;
  repositories: (root: string) => Promise<Repository[]>;
};

type State = {
  byFolder: Record<string, RepoEntry>;
  reposByRoot: Record<string, RepositoriesEntry>;
  /** Roots being scanned right now, for the refresh button's spinner. */
  scanning: Record<string, true>;
};

/**
 * Two objects with the same JSON are the same read: a window-focus refresh that
 * finds nothing changed must not hand every subscriber a new object to render.
 * The snapshots are small, so comparing text is cheaper than the render it saves.
 */
function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

export function createGitRepo(reads: GitReads) {
  const useStore = create<State>()(() => ({ byFolder: {}, reposByRoot: {}, scanning: {} }));

  // Git has one open repository at a time, so of two reads in flight the later
  // asked is the one about the repository that is open now; an earlier answer
  // arriving after it is about a repository Go has since left.
  let latest = 0;
  const reading = new Map<string, Promise<void>>();
  const scanning = new Map<string, Promise<void>>();

  function put(folder: string, entry: RepoEntry) {
    useStore.setState((state) => {
      const before = state.byFolder[folder];
      if (before && same(before, entry)) return state;
      const next = { ...state.byFolder };
      // Re-inserted, so the order of keys is the order of use and the first is the oldest.
      delete next[folder];
      next[folder] = entry;
      for (const key of Object.keys(next)) {
        if (Object.keys(next).length <= MAX_FOLDERS) break;
        delete next[key];
      }
      return { ...state, byFolder: next };
    });
  }

  /**
   * Reads the repository Git has open, as `folder`. `stamp` says what this read
   * is for — a revision, a window focus — and two callers asking for the same
   * stamp share one read, which is how the rail and the view, both reacting to
   * one change, cost one round trip between them.
   */
  function refresh(folder: string, stamp: string): Promise<void> {
    if (!folder) return Promise.resolve();
    const key = `${folder}#${stamp}`;
    const shared = reading.get(key);
    if (shared) return shared;
    const mine = ++latest;
    const run = Promise.all([
      reads.status(),
      reads.log(COMMIT_ROWS),
      // Neither is worth failing the read for: no guards means committing does
      // not ask, and no remotes means the network buttons stay off.
      reads.guards().catch((): Guard[] => []),
      reads.remotes().catch((): Remote[] => []),
    ])
      .then(([status, commits, guards, remotes]) => {
        if (mine !== latest) return;
        put(folder, { snapshot: { status, commits, guards, remotes }, error: "" });
      })
      .catch((e: Error) => {
        if (mine !== latest) return;
        const kept = useStore.getState().byFolder[folder]?.snapshot ?? null;
        put(folder, { snapshot: kept, error: e.message });
      })
      .finally(() => {
        reading.delete(key);
      });
    reading.set(key, run);
    return run;
  }

  /** The repositories at or under `root`. `force` reads again even if a scan of it is running. */
  function refreshRepositories(root: string, force = false): Promise<void> {
    if (!root) return Promise.resolve();
    const running = scanning.get(root);
    if (running && !force) return running;
    useStore.setState((state) => ({ ...state, scanning: { ...state.scanning, [root]: true } }));
    const run = reads
      .repositories(root)
      .then((repos): RepositoriesEntry => ({ repos, error: "" }))
      .catch((e: Error): RepositoriesEntry => {
        const kept = useStore.getState().reposByRoot[root]?.repos ?? [];
        return { repos: kept, error: e.message };
      })
      .then((entry) => {
        useStore.setState((state) => {
          const { [root]: _done, ...rest } = state.scanning;
          const before = state.reposByRoot[root];
          return {
            ...state,
            scanning: rest,
            reposByRoot: before && same(before, entry) ? state.reposByRoot : { ...state.reposByRoot, [root]: entry },
          };
        });
      })
      .finally(() => {
        if (scanning.get(root) === run) scanning.delete(root);
      });
    scanning.set(root, run);
    return run;
  }

  return { useStore, refresh, refreshRepositories };
}

export const gitRepo = createGitRepo({
  status: gitStatus,
  log: gitLog,
  guards: gitGuards,
  remotes: gitRemotes,
  repositories: gitRepositories,
});

/** What Git last read in `folder`; null until it has read it once. */
export function useGitRepo(folder: string): RepoEntry | null {
  return gitRepo.useStore((state) => state.byFolder[folder] ?? null);
}

/**
 * The repository read most recently, whichever it was. What the rail shows,
 * dimmed, in the moment between choosing a repository and its first read landing,
 * so the list changes rather than vanishing. Never shown as the new repository's
 * own: the caller knows it is carried.
 */
export function useLatestGitRepo(): RepoEntry | null {
  return gitRepo.useStore((state) => {
    const keys = Object.keys(state.byFolder);
    return keys.length === 0 ? null : state.byFolder[keys[keys.length - 1]];
  });
}

export const refreshGitRepo = gitRepo.refresh;
export const refreshRepositories = gitRepo.refreshRepositories;
