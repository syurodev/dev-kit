import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * Spec §6.1, §11: Wide store destructuring (`const { a, b } = useXStore()`)
 * subscribes the component to every state update of that store, causing
 * unnecessary re-renders.
 *
 * This ratchet ensures no component destructures a store directly without
 * a selector / `useShallow`.
 */

const BASELINE = 0;
const STORE_DESTRUCTURE_PATTERN = /\} = use[A-Za-z]*Store\(\)/;

const ROOT = join(import.meta.dir);

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path));
      continue;
    }
    if ((entry.endsWith(".tsx") || entry.endsWith(".ts")) && !entry.includes(".test.")) {
      out.push(path);
    }
  }
  return out;
}

describe("store subscription ratchet", () => {
  test("no files destructure zustand store without selector", () => {
    const files = sourceFiles(ROOT);
    const offendingFiles: string[] = [];

    for (const file of files) {
      const content = readFileSync(file, "utf8");
      if (STORE_DESTRUCTURE_PATTERN.test(content)) {
        offendingFiles.push(file.slice(ROOT.length + 1));
      }
    }

    expect(
      offendingFiles.length,
      `Found ${offendingFiles.length} files destructuring store directly without useShallow:\n${offendingFiles.join("\n")}`
    ).toBeLessThanOrEqual(BASELINE);
  });
});
