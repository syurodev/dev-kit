import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * A hook called only when some condition holds (`ready && useThing()`,
 * `state === "running" && cannotFinish(useSettleFacts(...))`, `a ? useX() : y`)
 * changes the component's hook count when the condition flips, and React throws
 * ("Rendered more/fewer hooks than during the previous render", minified as
 * `undefined is not an object (evaluating 'i.next')`). A server render draws a
 * component once and never sees the flip, so the transcript tests passed while
 * the real window crashed on the first tool call whose status changed
 * (2026-10-30, AgentToolActivity and AgentRunningCommand). Call the hook on its
 * own line, then use its result in the condition.
 *
 * This reads source text, so it only catches the one shape that caused the crash;
 * the transition tests (transcript/settleHookOrder.test.tsx) catch the rest of the
 * shapes for the components they mount.
 */
const SRC = join(import.meta.dir);
const SKIP_DIRS = new Set(["dev", "testing", "bindings"]);

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (!SKIP_DIRS.has(name)) out.push(...sourceFiles(path));
    } else if (/\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name)) {
      out.push(path);
    }
  }
  return out;
}

// `&&`, `||` or `?` followed by a hook call, bare or as an argument of another call.
const CONDITIONAL_HOOK = /(&&|\|\||\?)\s*!?\s*(?:[\w.]+\(\s*)?use[A-Z]\w*\(/;

describe("hook calls", () => {
  test("none is guarded by a condition", () => {
    const offenders: string[] = [];
    for (const file of sourceFiles(SRC)) {
      readFileSync(file, "utf8")
        .split("\n")
        .forEach((line, i) => {
          const code = line.replace(/\/\/.*$/, "");
          if (CONDITIONAL_HOOK.test(code)) offenders.push(`${file.slice(SRC.length + 1)}:${i + 1}  ${code.trim().slice(0, 120)}`);
        });
    }
    expect(offenders).toEqual([]);
  });
});
