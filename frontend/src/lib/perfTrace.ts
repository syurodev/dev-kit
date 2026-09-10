import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";

/**
 * The measurement the whole smoothness doc asks to come first (section 9): a
 * mark around every tab and module switch, and a record of every long task
 * with the module that was active when it ran.
 *
 * Subscribes to the store rather than being called from setActiveTab: keeping
 * the hook outside the store means the store carries no dev-only code, and a
 * subscription sees module switches (focusModule) the same way it sees tab
 * ones.
 *
 * Never in a production bundle: main.tsx imports this dynamically behind
 * `import.meta.env.DEV || localStorage.devkitPerf === "1"`, so the chunk is
 * only fetched when someone asks for it.
 */
export function installPerfTrace(): () => void {
  let active = "";

  const stop = useWorkbenchTabsStore.subscribe((state) => {
    const next = `${state.activeModuleId}/${state.modules[state.activeModuleId]?.activeTabId ?? ""}`;
    if (next === active) return;
    // measure() from the previous switch's mark, so the number is the time
    // between one switch being asked for and the next — the interval a
    // longtask below would have landed in.
    if (active) {
      try {
        performance.measure(`devkit:switch ${active} → ${next}`, `devkit:switch:${active}`);
      } catch {
        // The mark is gone (buffer cleared); the next one re-seeds it.
      }
    }
    active = next;
    performance.mark(`devkit:switch:${next}`);
  });

  let observer: PerformanceObserver | undefined;
  try {
    observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (entry.duration <= 16) continue;
        console.warn(
          `[perf] long task ${Math.round(entry.duration)}ms while "${active || "shell"}" was active`,
        );
      }
    });
    observer.observe({ entryTypes: ["longtask"] });
  } catch {
    // WebKit has no longtask observer; the switch marks still work.
  }

  return () => {
    stop();
    observer?.disconnect();
  };
}
