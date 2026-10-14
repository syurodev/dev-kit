import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (path: string) => readFileSync(join(import.meta.dir, path), "utf8");

describe("code takes its weight and links from the palette", () => {
  test("the lezer table weights keywords and colours links by hue", () => {
    const src = read("devkitSyntax.ts");
    expect(src).toContain('class: "text-(--syntax-keyword) font-(--syntax-keyword-weight)"');
    expect(src).toContain('class: "text-(--hue-blue) underline"');
    expect(src).toContain('class: "text-(--hue-blue)"');
    expect(src).not.toContain("--status-info");
  });

  test("both Plate code blocks weight hljs keywords", () => {
    for (const file of ["../ui/code-block-node.tsx", "../ui/code-block-node-static.tsx"]) {
      expect(read(file)).toContain(
        "**:[.hljs-keyword,.hljs-doctag,.hljs-template-tag,.hljs-selector-tag,.hljs-variable.language\\\\_]:font-(--syntax-keyword-weight)",
      );
    }
  });
});
