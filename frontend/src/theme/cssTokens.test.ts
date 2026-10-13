import { describe, expect, test } from "bun:test";
import { parseBlock, resolveVar, stripComments } from "./cssTokens";

const css = stripComments(`
:root {
    --a: #111111; /* trailing note --fake: #000000 */
    --b: var(--a);
    --c: var(--b);
}

:root[data-theme="light"] {
    --a: #eeeeee;
}
`);

describe("cssTokens", () => {
  test("parses one block and ignores comments", () => {
    const dark = parseBlock(css, /\n:root/);
    expect(dark.get("--a")).toBe("#111111");
    expect(dark.has("--fake")).toBe(false);
  });

  test("resolves a var chain, light falling back to dark", () => {
    const dark = parseBlock(css, /\n:root/);
    const light = parseBlock(css, /:root\[data-theme="light"\]/);
    expect(resolveVar("--c", dark, dark)).toBe("#111111");
    expect(resolveVar("--c", light, dark)).toBe("#eeeeee");
    expect(resolveVar("var(--b)", light, dark)).toBe("#eeeeee");
    expect(resolveVar("#123456", light, dark)).toBe("#123456");
  });
});
