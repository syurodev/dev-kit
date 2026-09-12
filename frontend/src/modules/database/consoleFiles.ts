import { deleteConsole, saveConsole, type StoredConsole } from "../../bridge/database";

/**
 * When a console's text reaches the disk.
 *
 * A console is written as you type, so the writes have to be coalesced or
 * every keystroke is a file rename. 600ms after you stop: long enough that a
 * sentence of SQL is one write, short enough that nothing is lost to a crash
 * you would blame the app for.
 *
 * The one rule that is not about performance: closing a console CANCELS its
 * pending write. Without that, closing a tab deletes the file and the debounce
 * that was already in flight writes it straight back — a console you closed,
 * returning after the next relaunch.
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

    /** The tab is gone: cancel whatever was queued, then remove the file. */
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
