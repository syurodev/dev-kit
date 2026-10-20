import { Events } from "@wailsio/runtime";

/**
 * Where performance lines go: the console, and — inside the desktop window —
 * the Go process, which prints them in the `wails3 dev` terminal. WebKit's Web
 * Inspector has to be opened per window and forgets on reload; the terminal is
 * already on screen.
 *
 * Eager and tiny on purpose: the Workbench's Profilers feed recordCommit on
 * every commit, so this cannot live in the lazily-loaded perfTrace chunk.
 */
export const PERF_EVENT = "devkit:perf";

export function perfLog(line: string): void {
  console.info(line);
  if (typeof window !== "undefined" && "_wails" in window) {
    // Fire and forget: a lost perf line is not worth an error.
    void Events.Emit(PERF_EVENT, line).catch(() => undefined);
  }
}

/** The Profiler around the whole Workbench; the others are zones inside it. */
export const WORKBENCH_PROFILER = "workbench";

const commits = new Map<string, number>();
let workbenchClock = 0;

/** React Profiler onRender: adds this commit's render time to its Profiler's total. */
export function recordCommit(id: string, _phase: string, actualDuration: number): void {
  commits.set(id, (commits.get(id) ?? 0) + actualDuration);
  if (id === WORKBENCH_PROFILER) workbenchClock += actualDuration;
}

/**
 * Workbench render time since the page loaded, never reset. takeCommits clears
 * what it hands out, so two readers would steal each other's numbers; a reader
 * that wants "how much React ran between two moments" subtracts two readings.
 */
export function workbenchRenderClock(): number {
  return workbenchClock;
}

/**
 * React render time since the last call — the Workbench total, and the zones
 * inside it, largest first, with whatever no zone covers as "other" — and
 * starts counting again.
 */
export function takeCommits(): { total: number; zones: string } {
  const total = commits.get(WORKBENCH_PROFILER) ?? 0;
  const zones = [...commits].filter(([id]) => id !== WORKBENCH_PROFILER);
  const covered = zones.reduce((sum, [, ms]) => sum + ms, 0);
  if (total - covered >= 1) zones.push(["other", total - covered]);
  commits.clear();
  return {
    total,
    zones: zones
      .filter(([, ms]) => ms >= 1)
      .sort((a, b) => b[1] - a[1])
      .map(([id, ms]) => `${id} ${Math.round(ms)}`)
      .join(", "),
  };
}
