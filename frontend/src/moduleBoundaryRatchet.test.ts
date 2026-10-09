import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, normalize } from "node:path";

/**
 * `internal/arch/arch_test.go` pins the dependency direction between Go
 * packages so a layering violation fails the build instead of rotting
 * silently as the codebase grows. `src/modules/*` has the same shape of
 * problem — a module reaching into another module's internals instead of
 * going through a shared package (stores/, lib/, components/) — with nothing
 * pinning it.
 *
 * This is a ratchet, not a hard "never" gate: seven such edges already exist
 * (measured 2026-09-22) and ripping them out is a separate, deliberate
 * refactor, not something this test should force. What it stops is a new one
 * appearing unnoticed. To add a legitimate new edge, add it to BASELINE with
 * the same justification style as the entries below; to close one, delete
 * its entry — the "not stale" test catches a baseline nobody lowered.
 */

// "from -> to": the number of files in `from` that import from `to`. Keep
// this at what measure() actually reports, never above it.
const BASELINE: Record<string, number> = {
  // The agent setup and runtime panels reuse Settings' own form controls —
  // both surfaces about the same underlying agent runtime, in the module
  // that owns the UI shell for it.
  "agenthub -> settings": 2,
  // CodeGraph plan comments resolve an agenthub session reference to jump to
  // its transcript; "Ask agent" shows the session it wrote into the way every
  // other caller does (agenthub/documents.ts showAgentSession: its tab in
  // Agent Hub, the Agent panel elsewhere).
  "codegraph -> agenthub": 2,
  // CodeGraph plan nodes show the git change that produced them; git's own
  // diff renderer is reused rather than copied.
  "codegraph -> git": 1,
  // The database module's ERD-style plan view reuses CodeGraph's diagram
  // primitives.
  "database -> codegraph": 1,
  // Settings' agent runtime tab reads agenthub's runtime types to render its
  // install/version UI.
  "settings -> agenthub": 1,
};

const ROOT = join(import.meta.dir, "modules");
const MODULES = readdirSync(ROOT).filter((entry) => statSync(join(ROOT, entry)).isDirectory());

function moduleFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...moduleFiles(path));
      continue;
    }
    if ((entry.endsWith(".ts") || entry.endsWith(".tsx")) && !entry.includes(".test.")) {
      out.push(path);
    }
  }
  return out;
}

// Matches both `from "spec"` (static/re-export) and `import("spec")` (lazy).
const IMPORT_SPEC = /(?:from\s+|import\s*\()\s*["']([^"']+)["']/g;

function moduleOf(absPath: string): string | null {
  const rel = absPath.slice(ROOT.length + 1);
  const segment = rel.split("/")[0];
  return MODULES.includes(segment) ? segment : null;
}

/** The cross-module edges actually in the tree today: "from -> to" -> files. */
function measure(): Map<string, string[]> {
  const edges = new Map<string, string[]>();
  for (const mod of MODULES) {
    for (const file of moduleFiles(join(ROOT, mod))) {
      const body = readFileSync(file, "utf8");
      for (const match of body.matchAll(IMPORT_SPEC)) {
        const spec = match[1];
        let targetAbs: string;
        if (spec.startsWith("@/modules/")) {
          targetAbs = join(ROOT, spec.slice("@/modules/".length));
        } else if (spec.startsWith(".")) {
          targetAbs = normalize(join(dirname(file), spec));
        } else {
          continue; // bare specifier (react, zustand, "@/stores/...", …)
        }
        const targetMod = moduleOf(targetAbs);
        if (!targetMod || targetMod === mod) continue;
        const key = `${mod} -> ${targetMod}`;
        const relFile = file.slice(ROOT.length + 1);
        const files = edges.get(key) ?? [];
        if (!files.includes(relFile)) files.push(relFile);
        edges.set(key, files);
      }
    }
  }
  return edges;
}

describe("module boundary ratchet", () => {
  const actual = measure();

  test("no module reaches into another module beyond the recorded baseline", () => {
    const violations: string[] = [];
    for (const [edge, files] of actual) {
      const allowed = BASELINE[edge] ?? 0;
      if (files.length > allowed) {
        violations.push(
          `${edge}: ${files.length} file(s) > baseline ${allowed}\n` +
            files.map((f) => `    ${f}`).join("\n"),
        );
      }
    }
    expect(
      violations,
      "A module imported another module's internals. Either route the shared " +
        "code through components/, stores/ or lib/ instead, or — if the " +
        "dependency is deliberate — add it to BASELINE in moduleBoundaryRatchet.test.ts " +
        "with the same one-line justification the existing entries carry:\n\n" +
        violations.join("\n\n"),
    ).toEqual([]);
  });

  // A baseline nobody lowers is a baseline nobody reads (same rule as
  // designSystemConformance.test.ts). Also catches an edge whose last file
  // was deleted or rewritten: the entry should go with it.
  test("the baseline is not stale", () => {
    const stale: string[] = [];
    for (const [edge, allowed] of Object.entries(BASELINE)) {
      const actualCount = actual.get(edge)?.length ?? 0;
      if (actualCount < allowed) {
        stale.push(`${edge}: now ${actualCount}, baseline says ${allowed}`);
      }
    }
    expect(stale, `lower or remove these baseline entries:\n${stale.join("\n")}`).toEqual([]);
  });
});
