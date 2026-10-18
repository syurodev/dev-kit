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

/**
 * Past this many models the picker gets a search box. A few (Claude's, Codex's)
 * are read at a glance; opencode lists every model of every provider that has
 * credentials, which is a list to search, not to scroll.
 */
export const MODEL_SEARCH_THRESHOLD = 8;

/**
 * The models that match what was typed: every word, in any order, anywhere in
 * the name or the id ("sonnet anth" finds "anthropic/Claude Sonnet"). Case is
 * ignored; an empty query keeps all of them, in the agent's own order.
 */
export function filterModelChoices<T extends { value: string; name?: string }>(choices: readonly T[], query: string): T[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return [...choices];
  return choices.filter((choice) => {
    const haystack = `${choice.name ?? ""} ${choice.value}`.toLowerCase();
    return terms.every((term) => haystack.includes(term));
  });
}
