/**
 * Coalesces writes per KEY, with a flush for a quit that cannot wait.
 *
 * One timer per key, not one for every key (#50): the ui_state storage shared a
 * single 400ms debounce across all eight stores that persist, so a folder
 * change followed inside the window by any other store's write — a tab opened —
 * was never saved at all.
 */
export function createDebouncedWriter<T>(write: (key: string, value: T) => void, delayMs: number) {
  const pending = new Map<string, { timer: ReturnType<typeof setTimeout>; value: T }>();
  return {
    schedule(key: string, value: T) {
      const existing = pending.get(key);
      if (existing) clearTimeout(existing.timer);
      pending.set(key, {
        value,
        timer: setTimeout(() => {
          pending.delete(key);
          write(key, value);
        }, delayMs),
      });
    },
    flush() {
      for (const [key, { timer, value }] of pending) {
        clearTimeout(timer);
        write(key, value);
      }
      pending.clear();
    },
  };
}
