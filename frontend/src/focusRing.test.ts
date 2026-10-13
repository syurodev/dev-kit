import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * Keyboard focus rings use --border-focus, not --status-info. The chrome is
 * Mono, so status-info is a grey: `ring-status-info/20` is a 20% grey at about
 * 1.4:1 and focus is all but invisible. A full-opacity `ring-status-info` is a
 * selected mark and stays allowed. The same goes for the focused border:
 * `border-border-focus`, not `border-status-info`.
 */
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "node_modules" ? [] : sources(path);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

describe("focus rings", () => {
  test("no source draws a translucent ring from status-info", () => {
    const offenders = sources(import.meta.dir).filter((file) =>
      /ring-status-info\/\d/.test(strip(readFileSync(file, "utf8"))),
    );
    expect(offenders.map((file) => file.slice(import.meta.dir.length + 1))).toEqual([]);
  });

  test("no source draws a focused border from status-info", () => {
    const offenders = sources(import.meta.dir).filter((file) =>
      /(?:focus-visible|focus-within|focus):border-status-info/.test(strip(readFileSync(file, "utf8"))),
    );
    expect(offenders.map((file) => file.slice(import.meta.dir.length + 1))).toEqual([]);
  });
});
