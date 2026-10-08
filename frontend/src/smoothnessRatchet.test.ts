import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * The smoothness counterpart to designSystemConformance: the four patterns
 * that produce most of the flashing and jank in this app, as numbers that can
 * only go down.
 *
 * These are deliberately coarse — a regex cannot tell a fetch in an effect
 * from a subscription, and it cannot tell a 12-row table from a 12 000-row
 * one. That is accepted (smoothness 9 asks for a rough estimate, not a
 * parser): the value is that the number moves when someone adds a fifteenth
 * one, and this file is where they have to notice.
 */

const ROOT = join(import.meta.dir);

function files(dir: string, ext: string[]): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...files(path, ext));
      continue;
    }
    // A test asserting a class is not a surface carrying it.
    if (entry.includes(".test.")) continue;
    if (ext.some((e) => entry.endsWith(e))) out.push(path);
  }
  return out;
}

const EMPTY_LIST_STATE = /useState(?:<[^>]*>)?\(\[\]\)/g;
const FETCH_IN_EFFECT = /useEffect\(/;
const ANIMATE_IN = /animate-in/g;
const MOTION_DURATION = /duration-\(--motion-/;

function count(pattern: RegExp, body: string): number {
  return body.match(new RegExp(pattern.source, "g"))?.length ?? 0;
}

/**
 * 2.1: a list that starts as [] cannot tell "not loaded yet" from "empty", so
 * the empty copy flashes on every first paint. Counted only where an effect
 * could be filling it.
 */
function emptyListState(): number {
  let total = 0;
  for (const file of files(join(ROOT, "modules"), [".tsx"])) {
    const body = readFileSync(file, "utf8");
    if (!FETCH_IN_EFFECT.test(body)) continue;
    total += count(EMPTY_LIST_STATE, body);
  }
  return total;
}

/** 6.3: a table built from a .map with no virtualizer behind it. */
function unvirtualizedTables(): number {
  let total = 0;
  for (const dir of ["modules", "components", "workbench"]) {
    for (const file of files(join(ROOT, dir), [".tsx"])) {
      const body = readFileSync(file, "utf8");
      if (!body.includes("<table")) continue;
      if (/useVirtual(Rows|izer)/.test(body)) continue;
      if (!body.includes(".map(")) continue;
      total += 1;
    }
  }
  return total;
}

/**
 * 7.1: glass costs GPU on every frame the thing under it changes.
 *
 * Only real blur counts. `backdrop-filter: none` is how a surface OPTS OUT —
 * the prefers-reduced-transparency block is entirely made of them — and prose
 * mentioning the property costs nothing at all; counting either made the
 * number go up when glass was being removed, which is backwards.
 *
 * The exclusion was written as `\s*(?!none)` and did not work: `\s*` gives
 * back the space it matched, the lookahead then reads " none", and " none"
 * does not begin with "none", so every opt-out counted. Adding a
 * reduced-transparency block for one new surface cost two. `(?!\s*none)`
 * refuses on both paths, which is what the paragraph above always claimed.
 */
function blurSurfaces(): number {
  let total = count(
    /backdrop-filter:\s*(?!\s*none)/,
    readFileSync(join(ROOT, "index.css"), "utf8"),
  );
  for (const dir of ["modules", "components", "workbench", "dev"]) {
    for (const file of files(join(ROOT, dir), [".tsx"])) {
      total += count(/backdrop-blur/, readFileSync(file, "utf8"));
    }
  }
  return total;
}

/**
 * A query default that allocates on every render: `const { data: x = [] } =
 * useQuery(...)` hands back a NEW array on every render where data is
 * undefined. Anything memoised on it recomputes, and anything that writes it
 * into a store makes the store churn — which is how NotesDashboard rendered
 * itself into "Maximum update depth exceeded". Use lib/stableEmpty instead.
 * At 0 this is a guard, not a ratchet.
 */
function unstableQueryDefaults(): number {
  let total = 0;
  for (const dir of ["modules", "components", "workbench", "dev"]) {
    for (const file of files(join(ROOT, dir), [".tsx", ".ts"])) {
      const body = readFileSync(file, "utf8");
      if (!body.includes("useQuery")) continue;
      total += count(/data:\s*\w+\s*=\s*(\[\]|\{\})/, body);
    }
  }
  return total;
}

/**
 * The same allocation in a store selector: `useStore((s) => s.byId[id] ?? [])`
 * returns a new array on every read while the key is missing, so zustand's
 * useSyncExternalStore sees a changed snapshot each time and React renders
 * forever ("The result of getSnapshot should be cached"). Agent Hub's queued
 * prompts did exactly this for any session with an empty queue. Use
 * lib/stableEmpty, or wrap the selector in useShallow. At 0 this is a guard.
 */
function unstableStoreSelectors(): number {
  let total = 0;
  for (const dir of ["modules", "components", "workbench", "dev", "stores"]) {
    for (const file of files(join(ROOT, dir), [".tsx", ".ts"])) {
      if (file.includes(".test.")) continue;
      total += count(/use\w*Store\(\s*\(?\s*\w+\s*\)?\s*=>[^;]{0,200}?\?\?\s*(\[\]|\{\})/, readFileSync(file, "utf8"));
    }
  }
  return total;
}

/** 4.1: an animation whose duration is not one of the three tokens. */
function untokenisedAnimateIn(): number {
  let total = 0;
  for (const dir of ["modules", "components", "workbench", "dev"]) {
    for (const file of files(join(ROOT, dir), [".tsx"])) {
      for (const line of readFileSync(file, "utf8").split("\n")) {
        if (!ANIMATE_IN.test(line)) continue;
        if (MOTION_DURATION.test(line)) continue;
        total += count(ANIMATE_IN, line);
      }
    }
  }
  return total;
}

// Baseline as of the second smoothness pass. Lower it when you convert
// something; it must never go up.
const BASELINE = {
  emptyListState: 6,
  unvirtualizedTables: 2,
  // 48 → 44 with the glass button already in it: four of the old 48 were
  // `backdrop-filter: none` opt-outs that the exclusion above never actually
  // excluded. 44 → 42: the plan inspector and Save Query drew their own
  // blurred overlay and now use Dialog's. 42 → 39: the dock and its two
  // gallery specs went; modules switch from the title bar. 39 → 37: Settings
  // drew its own blurred scrim and panel and is a Dialog now. 37 → 36: Agent
  // Hub's new-session dialog went — the agent is picked in the composer and
  // the folder comes from the project row or the native picker. 36 → 35: the
  // permission toast wore a hand-rolled backdrop-blur and now wears glass-float.
  // 35 → 33: Database New Connection/Delete use shared dialog overlays.
  // 33 → 23: shared database dialogs and opaque control/status surfaces.
  // 23 → 22: Agent metadata inspector uses the shared dialog material.
  blurSurfaces: 22,
  untokenisedAnimateIn: 3,
  unstableQueryDefaults: 0,
  unstableStoreSelectors: 0,
};

const MEASURES = {
  emptyListState,
  unvirtualizedTables,
  blurSurfaces,
  untokenisedAnimateIn,
  unstableQueryDefaults,
  unstableStoreSelectors,
} as const;

describe("smoothness ratchet", () => {
  for (const [name, measure] of Object.entries(MEASURES)) {
    test(`${name} does not grow`, () => {
      const actual = measure();
      expect(
        actual,
        `${name}: ${actual} > baseline ${BASELINE[name as keyof typeof BASELINE]}. ` +
          `See docs/dev-kit/smoothness.mdx for what this pattern costs, or — if ` +
          `you removed some — lower the baseline in this file.`,
      ).toBeLessThanOrEqual(BASELINE[name as keyof typeof BASELINE]);
    });
  }

  // A baseline nobody lowers is a baseline nobody reads.
  test("the baseline is not stale", () => {
    const stale: string[] = [];
    for (const [name, measure] of Object.entries(MEASURES)) {
      const actual = measure();
      const owed = BASELINE[name as keyof typeof BASELINE];
      if (actual < owed) stale.push(`${name}: ${actual} < recorded ${owed}`);
    }
    expect(stale, `Lower these in BASELINE:\n- ${stale.join("\n- ")}`).toEqual([]);
  });
});
