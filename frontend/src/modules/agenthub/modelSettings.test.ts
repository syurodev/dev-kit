import { describe, expect, test } from "bun:test";
import { MODEL_SEARCH_THRESHOLD, draftConfigOptions, filterModelChoices, initialModelConfig, reasoningLabel } from "./modelSettings";
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

describe("searching a long model list", () => {
  const models = [
    { value: "anthropic/sonnet", name: "anthropic/Claude Sonnet" },
    { value: "anthropic/haiku", name: "anthropic/Claude Haiku" },
    { value: "openai/gpt-pro", name: "openai/GPT Pro" },
    { value: "openrouter/qwen3-coder", name: "openrouter/Qwen 3 Coder" },
    { value: "bare-id" },
  ];

  test("no query keeps everything, in the agent's order", () => {
    expect(filterModelChoices(models, "")).toEqual(models);
    expect(filterModelChoices(models, "   ")).toEqual(models);
    expect(filterModelChoices(models, "")).not.toBe(models);
  });

  test("a word matches the name or the id, ignoring case", () => {
    expect(filterModelChoices(models, "SONNET").map((m) => m.value)).toEqual(["anthropic/sonnet"]);
    expect(filterModelChoices(models, "gpt-pro").map((m) => m.value)).toEqual(["openai/gpt-pro"]);
    expect(filterModelChoices(models, "bare").map((m) => m.value)).toEqual(["bare-id"]);
  });

  test("every word has to match, in any order", () => {
    expect(filterModelChoices(models, "claude anthropic").map((m) => m.value)).toEqual(["anthropic/sonnet", "anthropic/haiku"]);
    expect(filterModelChoices(models, "haiku claude").map((m) => m.value)).toEqual(["anthropic/haiku"]);
    expect(filterModelChoices(models, "claude gpt")).toEqual([]);
  });

  test("a provider prefix narrows to that provider's models", () => {
    expect(filterModelChoices(models, "openrouter/").map((m) => m.value)).toEqual(["openrouter/qwen3-coder"]);
  });

  test("a short list is not searched", () => {
    expect(MODEL_SEARCH_THRESHOLD).toBeGreaterThan(4);
  });
});
