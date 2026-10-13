import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * Categorical colour reads the Color palette's hues, never status: the chrome
 * is Mono, so status is reserved for how safe something is. These files were
 * audited when the split landed (docs/superpowers/specs/2026-10-13-…).
 */
const FILES = [
  "modules/codegraph/graph/tokens.ts",
  "modules/codegraph/graph/ConnectorEdge.tsx",
  "modules/database/icons/engineMarks.tsx",
];

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

describe("decorative colour is a hue", () => {
  for (const file of FILES) {
    test(file, () => {
      const src = strip(readFileSync(join(import.meta.dir, file), "utf8"));
      expect(src).not.toMatch(/status-(info|success|warning|recovery)/);
      expect(src).toMatch(/hue-/);
    });
  }

  // The "dashed = plan not satisfied" swatch matches the plan nodes' neutral
  // dashed border, so the legend needs no hue of its own for it.
  test("layer legend carries no status-info", () => {
    const src = strip(readFileSync(join(import.meta.dir, "modules/codegraph/graph/LayerLegend.tsx"), "utf8"));
    expect(src).not.toContain("status-info");
  });

  // requestModel.ts also colours HTTP status CODES (2xx/3xx/5xx), which is
  // semantic and stays on status; only the method tone is categorical.
  test("API client method tone", () => {
    const src = strip(readFileSync(join(import.meta.dir, "modules/apiclient/requestModel.ts"), "utf8"));
    const body = src.slice(src.indexOf("export function methodTone"), src.indexOf("}\n", src.indexOf("export function methodTone")));
    expect(body).not.toMatch(/status-/);
    expect(body).toMatch(/hue-/);
  });

  test("plan diagram steps and ERD keys are hues", () => {
    const plan = strip(readFileSync(join(import.meta.dir, "modules/database/plan/PlanDiagram.tsx"), "utf8"));
    expect(plan).not.toMatch(/cssVar: "--status-/);
    const erd = strip(readFileSync(join(import.meta.dir, "modules/database/components/ERDView.tsx"), "utf8"));
    expect(erd).not.toMatch(/status-info/);
  });

  const read = (file: string) => strip(readFileSync(join(import.meta.dir, file), "utf8"));

  test("git's untracked mark is the blue hue", () => {
    const src = read("modules/git/GitParts.tsx");
    const block = src.slice(src.indexOf("untracked: {"), src.indexOf("renamed: {"));
    expect(block).toContain("bg-(--hue-blue)");
    expect(block).not.toContain("status-info");
  });

  test("git's renamed mark is the violet hue", () => {
    const src = read("modules/git/GitParts.tsx");
    const start = src.indexOf("renamed: {");
    const block = src.slice(start, src.indexOf("}", start));
    expect(block).toContain("bg-(--hue-violet)");
    expect(block).not.toContain("status-recovery");
  });

  test("Files tab renamed mark is the violet hue", () => {
    const line = read("modules/codegraph/sidebar/FilesTab.tsx")
      .split("\n")
      .find((l) => l.trim().startsWith("R:"));
    expect(line).toContain("hue-violet");
  });

  test("data-flow read highlight is the blue hue", () => {
    const src = read("modules/codegraph/inspector/DataFlowTab.tsx");
    expect(src).toContain("text-(--hue-blue)");
    expect(src).not.toContain("text-status-info");
  });

  test("result grid key icon is the yellow hue, like the ERD's primary key", () => {
    const src = read("modules/database/grid/ResultGrid.tsx");
    expect(src).toContain("text-(--hue-yellow)");
    expect(src).not.toContain("isKey ? \"text-status-info\"");
  });
});
