import { describe, expect, test } from "bun:test";
import { draftConfigOptions, initialModelConfig, reasoningLabel } from "./modelSettings";
import type { ConfigOption } from "./types";

const catalog: ConfigOption[] = [{ id: "model", category: "model", currentValue: "sol", defaultValue: "sol", options: [
  { value: "sol", name: "Sol", defaultEffort: "medium", supportedEfforts: [{ value: "low" }, { value: "medium" }, { value: "high" }] },
  { value: "luna", name: "Luna", defaultEffort: "low", supportedEfforts: [{ value: "low" }] },
] }];

describe("draft model and reasoning choices", () => {
  test("the first message gets the chosen model and effort", () => {
    expect(initialModelConfig(draftConfigOptions(catalog, { model: "sol", reasoning_effort: "high" })))
      .toEqual({ model: "sol", reasoning_effort: "high" });
  });
  test("changing model discards an unsupported effort", () => {
    expect(initialModelConfig(draftConfigOptions(catalog, { model: "luna", reasoning_effort: "high" })))
      .toEqual({ model: "luna", reasoning_effort: "low" });
  });
  test("a refreshed catalog dropping the selected model uses the catalog default", () => {
    expect(initialModelConfig(draftConfigOptions(catalog, { model: "removed", reasoning_effort: "medium" })))
      .toEqual({ model: "sol", reasoning_effort: "medium" });
  });
  test("absent effort capability creates no effort control", () => {
    const options = draftConfigOptions([{ id: "model", options: [{ value: "plain" }] }], {});
    expect(options).toHaveLength(1);
    expect(initialModelConfig(options)).toEqual({ model: "plain" });
    expect(reasoningLabel("xhigh")).toBe("Extra high");
  });
});
