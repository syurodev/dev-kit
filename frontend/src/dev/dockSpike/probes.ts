import { useSyncExternalStore } from "react";

/**
 * SPIKE — what the checklist measures, as a tiny external store so the probe
 * card re-renders without the panels doing so.
 */
export type Probes = {
  /** React mounts per panel id. A move that re-mounts shows up as > 1. */
  mounts: Record<string, number>;
  /** xterm instances ever constructed. Must stay at one per terminal. */
  terminalsCreated: number;
  /** Last canvas size seen after a resize, per panel. 0 = measured hidden. */
  canvasSize: Record<string, { w: number; h: number }>;
  lastSwitchMs: number | null;
  /** Did a companion's rect change across the last module switch? */
  companionMovedOnSwitch: boolean | null;
  blockedDrops: number;
  lastRestore: "ok" | "recovered" | null;
};

let state: Probes = {
  mounts: {},
  terminalsCreated: 0,
  canvasSize: {},
  lastSwitchMs: null,
  companionMovedOnSwitch: null,
  blockedDrops: 0,
  lastRestore: null,
};
const listeners = new Set<() => void>();

export function updateProbes(patch: (s: Probes) => Partial<Probes>) {
  state = { ...state, ...patch(state) };
  for (const l of listeners) l();
}

// Distinct component instances per panel, not effect runs: StrictMode runs a
// mount effect twice on the same instance in dev, which is not a re-mount.
const instances = new Map<string, Set<number>>();
let seq = 0;
export const nextInstance = () => ++seq;

export function countMount(id: string, instance: number) {
  const set = instances.get(id) ?? new Set<number>();
  set.add(instance);
  instances.set(id, set);
  updateProbes((s) => ({ mounts: { ...s.mounts, [id]: set.size } }));
}

/** A removed panel's next mount is a new panel, not a re-mount by a move. */
export function forgetMounts(id: string) {
  instances.delete(id);
  updateProbes((s) => {
    const { [id]: _, ...rest } = s.mounts;
    return { mounts: rest };
  });
}

export function useProbes(): Probes {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => state,
  );
}
