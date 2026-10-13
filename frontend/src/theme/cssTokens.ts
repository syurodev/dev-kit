/**
 * Reading index.css's token blocks — for tests only.
 *
 * tokenContract.test.ts and themeContrast.test.ts both need the stylesheet's
 * declarations as data; this is the one parser they share, so a fix to it
 * (comments, var chains) reaches both.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** Comments go first: index.css documents tokens in prose that starts with
 *  the token's own name, and a parser that reads those invents tokens. */
export function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, "");
}

export function readIndexCss(): string {
  return stripComments(readFileSync(join(import.meta.dir, "../index.css"), "utf8"));
}

/** The declarations inside the first `{ … }` block after `selector`. */
export function parseBlock(css: string, selector: RegExp): Map<string, string> {
  const match = css.match(new RegExp(selector.source + String.raw`\s*\{([\s\S]*?)\n\}`));
  if (!match) throw new Error(`no block for ${selector}`);
  const out = new Map<string, string>();
  for (const line of match[1].split("\n")) {
    const declaration = line.trim();
    if (!declaration.startsWith("--")) continue;
    const colon = declaration.indexOf(":");
    if (colon === -1) continue;
    out.set(declaration.slice(0, colon).trim(), declaration.slice(colon + 1).split(";")[0].trim());
  }
  return out;
}

/**
 * A token name or a value, followed through `var(--x)` until it is not one.
 * `mode` is the block for the mode being read; `base` is :root, which every
 * mode inherits from.
 */
export function resolveVar(
  tokenOrValue: string,
  mode: Map<string, string>,
  base: Map<string, string>,
): string | undefined {
  let value: string | undefined = tokenOrValue.startsWith("--")
    ? (mode.get(tokenOrValue) ?? base.get(tokenOrValue))
    : tokenOrValue;
  for (let hop = 0; hop < 8 && value?.startsWith("var("); hop += 1) {
    const name = value.slice(4, value.indexOf(")"));
    value = mode.get(name) ?? base.get(name);
  }
  return value;
}

/** index.css's dark (:root) and light blocks. */
export function themeBlocks() {
  const css = readIndexCss();
  return {
    dark: parseBlock(css, /\n:root/),
    light: parseBlock(css, /:root\[data-theme="light"\]/),
  };
}
