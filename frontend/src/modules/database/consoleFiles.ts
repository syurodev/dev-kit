import { deleteConsole, saveConsole, type StoredConsole } from "../../bridge/database";

/**
 * When a console's text reaches the disk.
 *
 * A console is written as you type, so the writes have to be coalesced or
 * every keystroke is a file rename. 600ms after you stop: long enough that a
 * sentence of SQL is one write, short enough that nothing is lost to a crash
 * you would blame the app for.
 *
 * Closing a tab and deleting a console are different things, and the
 * difference is the whole reason the Consoles rail can exist. Closing
 * RELEASES: whatever was queued is written, and the file stays for the rail
 * to list. Deleting FORGETS: the queued write is cancelled first, because
 * without that the debounce already in flight writes the file straight back
 * after the unlink and a console you deleted returns on the next launch.
 */
export type ConsoleFile = Omit<StoredConsole, "updatedAt">;

type ConsoleIO = {
  save: (console: ConsoleFile) => Promise<void>;
  remove: (id: string) => Promise<void>;
};

export function createConsoleFiles(io: ConsoleIO, delayMs = 600) {
  const timers = new Map<string, ReturnType<typeof setTimeout>>();
  const pending = new Map<string, ConsoleFile>();

  const write = (id: string) => {
    timers.delete(id);
    const file = pending.get(id);
    pending.delete(id);
    if (!file) return;
    // Swallowed on purpose. This runs on a timer nobody awaited, and a
    // rejected promise with no catch is an unhandled rejection in the window;
    // the backend has already logged the cause. The console keeps working —
    // it just will not be there after a relaunch, which is the honest outcome
    // of a disk that would not take it.
    void io.save(file).catch(() => undefined);
  };

  return {
    /** Queue this console's text. The last call inside the window wins. */
    schedule(file: ConsoleFile) {
      pending.set(file.id, file);
      const existing = timers.get(file.id);
      if (existing) clearTimeout(existing);
      timers.set(
        file.id,
        setTimeout(() => write(file.id), delayMs),
      );
    },

    /**
     * The tab is gone, the console is not: write what was queued and let go.
     *
     * The last thing typed is usually inside the debounce window — you type,
     * you look at it, you close the tab — so a release that only cancelled
     * would lose exactly the edit the user made last.
     */
    release(id: string) {
      const existing = timers.get(id);
      if (!existing) return;
      clearTimeout(existing);
      write(id);
    },

    /** Deleted on purpose: cancel the queued write, then remove the file. */
    forget(id: string) {
      const existing = timers.get(id);
      if (existing) clearTimeout(existing);
      timers.delete(id);
      pending.delete(id);
      void io.remove(id).catch(() => undefined);
    },

    /** For tests, and for a quit that cannot wait out the debounce. */
    flush() {
      for (const id of [...timers.keys()]) {
        clearTimeout(timers.get(id));
        write(id);
      }
    },
  };
}

export const consoleFiles = createConsoleFiles({ save: saveConsole, remove: deleteConsole });
