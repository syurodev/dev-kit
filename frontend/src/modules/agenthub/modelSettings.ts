import type { ConfigOption } from "./types";

/** Draft selections are local until the first session is created. */
export function draftConfigOptions(catalog: ConfigOption[], selection: Record<string, string>): ConfigOption[] {
  const model = catalog.find((option) => option.category === "model" || option.id === "model");
  if (!model) return catalog;
  const selected = model.options?.find((choice) => choice.value === selection[model.id])
    ?? model.options?.find((choice) => choice.value === model.currentValue)
    ?? model.options?.[0];
  const result = catalog.filter((option) => option.id !== "reasoning_effort").map((option) =>
    option === model ? { ...option, currentValue: selected?.value } : option,
  );
  if (selected?.supportedEfforts?.length) {
    const effort = selected.supportedEfforts.some((choice) => choice.value === selection.reasoning_effort)
      ? selection.reasoning_effort : selected.defaultEffort ?? selected.supportedEfforts[0]?.value;
    result.push({ id: "reasoning_effort", name: "Reasoning effort", category: "thought_level", type: "select",
      currentValue: effort, defaultValue: selected.defaultEffort, options: selected.supportedEfforts });
  }
  return result;
}

export function initialModelConfig(options: ConfigOption[]): Record<string, string> {
  return Object.fromEntries(options.filter((option) =>
    (option.id === "model" || option.id === "reasoning_effort") && typeof option.currentValue === "string",
  ).map((option) => [option.id, option.currentValue as string]));
}

export function reasoningLabel(value: string): string {
  return ({ xhigh: "Extra high", max: "Max", ultra: "Ultra" } as Record<string, string>)[value]
    ?? value.charAt(0).toUpperCase() + value.slice(1);
}
