import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { perfLog, takeCommits } from "./perfLog";

/**
 * The measurement the whole smoothness doc asks to come first (section 9): how
 * long every tab and module switch takes to reach the screen, and every long
 * task with the module that was active when it ran.
 *
 * A switch is timed from the store change to the frame after the one that
 * painted it (two requestAnimationFrames), which works in WebKit — the engine
 * the app actually ships in, and one with no longtask observer. The React part
 * of that time comes from the Workbench's Profiler. Lines go through perfLog,
 * so they print in the `wails3 dev` terminal:
 *
 *   [perf] switch notes › Meeting notes → database › orders.sql: 132ms to paint,
 *     12ms React (views 7, rail 3, tabs 1, titlebar 1)
 *
 * "To paint" is the first paint. A module visited for the first time paints its
 * skeleton then, and ModuleSkeleton logs how long the real view took after it.
 *
 * Subscribes to the store rather than being called from setActiveTab, so the
 * store carries no dev-only code and module switches (focusModule) are seen the
 * same way tab ones are.
 *
 * Never in a production bundle: main.tsx imports this dynamically behind
 * `import.meta.env.DEV || localStorage.devkitPerf === "1"`, so the chunk is
 * only fetched when someone asks for it.
 */
export function installPerfTrace(): () => void {
  const describe = () => {
    const state = useWorkbenchTabsStore.getState();
    const moduleTabs = state.modules[state.activeModuleId];
    const tab = moduleTabs?.openTabs.find((candidate) => candidate.id === moduleTabs.activeTabId);
    return tab ? `${state.activeModuleId} › ${tab.title}` : state.activeModuleId || "shell";
  };

  let active = describe();
  let pending = 0;

  const stop = useWorkbenchTabsStore.subscribe(() => {
    const next = describe();
    if (next === active) return;
    const from = active;
    active = next;

    const start = performance.now();
    takeCommits();
    // A newer switch before this one painted supersedes it: its number would
    // be the sum of both.
    const token = ++pending;
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        if (token !== pending) return;
        const end = performance.now();
        const name = `devkit:switch ${from} → ${next}`;
        try {
          performance.measure(name, { start, end });
        } catch {
          // Older engines without the options form; the log line still goes out.
        }
        const react = takeCommits();
        perfLog(
          `[perf] switch ${from} → ${next}: ${Math.round(end - start)}ms to paint, ` +
            `${Math.round(react.total)}ms React${react.zones ? ` (${react.zones})` : ""}`,
        );
      }),
    );
  });

  let observer: PerformanceObserver | undefined;
  try {
    observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (entry.duration <= 16) continue;
        perfLog(`[perf] long task ${Math.round(entry.duration)}ms while "${active}" was active`);
      }
    });
    observer.observe({ entryTypes: ["longtask"] });
  } catch {
    // WebKit has no longtask observer; the switch timings still work.
  }

  return () => {
    stop();
    observer?.disconnect();
  };
}
