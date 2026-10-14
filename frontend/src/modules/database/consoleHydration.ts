import { listConsoles, type StoredConsole } from "@/bridge/database";
import { useDatabaseStore } from "./databaseStore";

type Loader = () => Promise<StoredConsole[]>;

let hydration: Promise<void> | null = null;

/**
 * Reads the console files back into the consoles nobody has typed in, once
 * per window — shared by the module's rail and by anything that opens a
 * console before the rail has mounted (Open in Database from Agent Hub,
 * right after a relaunch). Without it such an open sees an empty slice where
 * a saved console is, writes into it, and the write lands on the file.
 *
 * A read that failed is not remembered as done: the next caller tries again.
 */
export function ensureConsolesHydrated(load: Loader = listConsoles): Promise<void> {
  if (!hydration) {
    let failed = false;
    const current = useDatabaseStore
      .getState()
      .hydrateConsoles(async () => {
        try {
          return await load();
        } catch (cause) {
          failed = true;
          throw cause;
        }
      })
      .then(() => {
        if (failed && hydration === current) hydration = null;
      });
    hydration = current;
  }
  return hydration;
}

/** Whether the once-per-window read has started (or finished). */
export function consolesHydrationStarted(): boolean {
  return hydration !== null;
}

/** For tests: forget that the files were read. */
export function resetConsoleHydration(): void {
  hydration = null;
}
