import { describe, expect, test } from "bun:test";
import { DEVKIT_FAMILY, FAMILIES, resolvePalette, resolvedPalettes } from "./families";
import { CHROME_TOKENS, HUE_TOKENS, PALETTE_TOKENS, SYNTAX_TOKENS, TERMINAL_TOKENS } from "./tokens";

describe("resolvePalette", () => {
  const dracula = FAMILIES.dracula.dark;

  test("fills every palette token and nothing else", () => {
    const resolved = resolvePalette(dracula);
    expect(Object.keys(resolved).sort()).toEqual([...PALETTE_TOKENS].sort());
  });

  test("derives syntax from hues when the family names none", () => {
    const r = resolvePalette(dracula);
    expect(r["--syntax-keyword"]).toBe(dracula["--hue-violet"]);
    expect(r["--syntax-string"]).toBe(dracula["--hue-green"]);
    expect(r["--syntax-number"]).toBe(dracula["--hue-orange"]);
    expect(r["--syntax-type"]).toBe(dracula["--hue-yellow"]);
    expect(r["--syntax-function"]).toBe(dracula["--hue-blue"]);
    expect(r["--syntax-definition"]).toBe("var(--text-primary)");
    expect(r["--syntax-comment"]).toBe("var(--text-tertiary)");
    expect(r["--syntax-meta"]).toBe("var(--text-tertiary)");
    expect(r["--syntax-keyword-weight"]).toBe("400");
  });

  test("a family's own syntax wins over the derivation", () => {
    const r = resolvePalette(FAMILIES.xcode.dark);
    expect(r["--syntax-keyword"]).toBe("#fc5fa3");
  });

  test("ANSI follows hues", () => {
    const r = resolvePalette(dracula);
    expect(r["--ansi-red"]).toBe(dracula["--hue-red"]);
    expect(r["--ansi-magenta"]).toBe(dracula["--hue-violet"]);
    expect(r["--ansi-cyan"]).toBe(dracula["--hue-teal"]);
  });

  test("devkit is Mono code with bold keywords", () => {
    const r = resolvePalette(DEVKIT_FAMILY.dark);
    expect(r["--syntax-keyword"]).toBe("var(--text-primary)");
    expect(r["--syntax-keyword-weight"]).toBe("600");
  });

  test("every family and mode resolves to non-empty strings", () => {
    for (const family of Object.values(FAMILIES)) {
      for (const mode of ["dark", "light"] as const) {
        for (const [token, value] of Object.entries(resolvePalette(family[mode]))) {
          expect(typeof value, `${family.id}/${mode} ${token}`).toBe("string");
          expect(value.length, `${family.id}/${mode} ${token}`).toBeGreaterThan(0);
        }
      }
    }
  });

  test("resolvedPalettes gives both modes", () => {
    const both = resolvedPalettes("nord");
    expect(both.dark["--hue-blue"]).toBe(FAMILIES.nord.dark["--hue-blue"]);
    expect(both.light["--hue-blue"]).toBe(FAMILIES.nord.light["--hue-blue"]);
  });
});

describe("every family is a palette, not a theme", () => {
  for (const family of Object.values(FAMILIES)) {
    for (const mode of ["dark", "light"] as const) {
      test(`${family.id}/${mode} declares all eight hues`, () => {
        const missing = HUE_TOKENS.filter((t) => !(t in family[mode]));
        expect(missing).toEqual([]);
      });

      test(`${family.id}/${mode} declares no chrome token`, () => {
        const chrome = new Set<string>(CHROME_TOKENS);
        expect(Object.keys(family[mode]).filter((t) => chrome.has(t))).toEqual([]);
      });

      test(`${family.id}/${mode} sets all syntax tokens or none`, () => {
        const set = SYNTAX_TOKENS.filter((t) => t in family[mode]);
        expect([0, SYNTAX_TOKENS.length]).toContain(set.length);
      });

      test(`${family.id}/${mode} leaves ANSI to the hues`, () => {
        expect(TERMINAL_TOKENS.filter((t) => t in family[mode])).toEqual([]);
      });
    }
  }
});
