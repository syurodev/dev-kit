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
  "modules/codegraph/graph/LayerLegend.tsx",
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
});
