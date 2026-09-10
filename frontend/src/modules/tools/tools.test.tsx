import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { TOOLS, toolById } from "./toolCatalogue";
import { ToolsSidebar } from "./ToolsSidebar";

const REPO = join(import.meta.dir, "../../../..");

/**
 * The ids in toolCatalogue.ts are the ones the bridge sends, and Go refuses an
 * id it does not know (CapabilityFor returns false). A typo on either side is a
 * tool that looks fine and fails at the moment someone presses Run, so the two
 * lists are compared directly rather than trusted.
 */
describe("catalogue matches the Go command table", () => {
  const commandsGo = readFileSync(join(REPO, "internal/tools/commands.go"), "utf8");
  const goIDs = [...commandsGo.matchAll(/Command\w+\s*=\s*"([^"]+)"/g)].map((m) => m[1]);

  it("finds the Go ids at all, so this test cannot pass vacuously", () => {
    // Not a hardcoded count: the two "every X has a Y" tests below are the real
    // check, and this one only has to prove the regex found something and that
    // the two sides are the same size. A literal here would need editing every
    // time a tool is added, which is how a guard turns into a chore.
    expect(goIDs.length).toBeGreaterThan(0);
    expect(goIDs.length).toBe(TOOLS.length);
  });

  it("every Go command has a catalogue entry", () => {
    for (const id of goIDs) {
      expect(toolById(id), `${id} is registered in Go but missing from toolCatalogue.ts`).toBeDefined();
    }
  });

  it("every catalogue entry has a Go command", () => {
    for (const tool of TOOLS) {
      expect(goIDs, `${tool.id} is in the catalogue but not registered in Go`).toContain(tool.id);
    }
  });

  // The module registry validates ids against this pattern and refuses the
  // manifest otherwise, which takes the whole app down at startup.
  it("ids satisfy the module registry's identifier rule", () => {
    const pattern = /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/;
    for (const tool of TOOLS) {
      expect(pattern.test(tool.id), `${tool.id} would be rejected by internal/modules`).toBe(true);
    }
  });
});

describe("the catalogue is coherent", () => {
  it("gives every tool a distinct id and a non-empty blurb", () => {
    const ids = new Set(TOOLS.map((t) => t.id));
    expect(ids.size).toBe(TOOLS.length);
    for (const tool of TOOLS) {
      expect(tool.name.length).toBeGreaterThan(0);
      expect(tool.blurb.length).toBeGreaterThan(0);
    }
  });

  it("gives every tool that takes input a placeholder, and the generator none", () => {
    for (const tool of TOOLS) {
      if (tool.shape === "generator") {
        expect(tool.inputPlaceholder, `${tool.id} generates, so it has no input to describe`).toBeUndefined();
      } else {
        expect(tool.inputPlaceholder, `${tool.id} takes input but does not say what`).toBeTruthy();
      }
    }
  });
});

describe("the rail", () => {
  const html = renderToString(<ToolsSidebar />);

  it("lists every tool", () => {
    for (const tool of TOOLS) {
      expect(html).toContain(tool.name);
      expect(html).toContain(tool.id);
    }
  });

  it("draws an icon on every row", () => {
    // lucide renders an <svg> per icon; one per row plus the search glyph.
    const svgs = html.match(/<svg/g)?.length ?? 0;
    expect(svgs).toBeGreaterThanOrEqual(TOOLS.length + 1);
  });

  it("marks the selected row with the shared accent, not a bespoke style", () => {
    expect(html).toContain("row-accent");
    expect(html).toContain('data-accent="on"');
  });

  it("no longer splits the list into implemented and designed", () => {
    expect(html).not.toContain("Design");
    expect(html).not.toContain("not built yet");
  });
});
