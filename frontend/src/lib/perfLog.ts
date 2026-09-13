import { Events } from "@wailsio/runtime";

/**
 * Where performance lines go: the console, and — inside the desktop window —
 * the Go process, which prints them in the `wails3 dev` terminal. WebKit's Web
 * Inspector has to be opened per window and forgets on reload; the terminal is
 * already on screen.
 *
 * Eager and tiny on purpose: the Workbench's Profiler feeds recordCommit on
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

let commitMs = 0;

/** React Profiler onRender: adds this commit's render time to the running total. */
export function recordCommit(_id: string, _phase: string, actualDuration: number): void {
  commitMs += actualDuration;
}

/** The React render time since the last call, and starts counting again. */
export function takeCommitMs(): number {
  const total = commitMs;
  commitMs = 0;
  return total;
}
