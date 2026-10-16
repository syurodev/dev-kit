import type { ModelProfile, ModelProfileCheck } from "../../bridge/agenthub";

/**
 * The part of agenthub's ConfigSelectOption / ConfigOption (modules/agenthub/
 * types.ts) the profile form reads — spelled here so Settings does not import
 * another module's types for it; the real types are assignable to these.
 */
export type ConfigSelectOption = { value: string; name?: string; description?: string; defaultEffort?: string; supportedEfforts?: ConfigSelectOption[] };
export type ConfigOption = { id: string; category?: string; options?: ConfigSelectOption[] };

/** The limits Go holds the list to (internal/preferences/modelprofiles.go). */
export const PROFILE_LIMITS = { profiles: 24, name: 60, description: 400, notes: 800 } as const;

/** What a vendor's picker offers, as the profile form needs it. */
export type VendorModelChoices =
  | { state: "loading" }
  | { state: "ready"; models: ConfigSelectOption[] }
  /** The list could not be read — signed out, not installed, … — and why. */
  | { state: "unavailable"; reason: string };

/** The model choices in a config catalog: its "model" option's own list. */
export function modelChoicesOf(catalog: ConfigOption[] | undefined): ConfigSelectOption[] {
  const model = catalog?.find((option) => option.id === "model" || option.category === "model");
  return model?.options ?? [];
}

/** The efforts one model takes, in the order its vendor lists them. */
export function effortChoicesOf(models: ConfigSelectOption[], model: string): ConfigSelectOption[] {
  return models.find((choice) => choice.value === model)?.supportedEfforts ?? [];
}

export function blankProfile(vendor: string): ModelProfile {
  return { id: "", name: "", description: "", vendor, model: "", effort: "", notes: "" };
}

/**
 * What is wrong with a profile the person is about to save, in their words, or
 * "" when nothing is. Go checks the same limits and has the last word; this is
 * so a form says so before the round trip.
 */
export function profileProblem(draft: ModelProfile, others: readonly ModelProfile[]): string {
  const name = draft.name.trim();
  if (!name) return "Give the profile a name — the job it is for.";
  if (name.length > PROFILE_LIMITS.name) return `Keep the name under ${PROFILE_LIMITS.name} characters.`;
  if (!draft.vendor.trim()) return "Choose the agent.";
  if (!draft.model.trim()) return "Choose a model.";
  if (draft.description.length > PROFILE_LIMITS.description) return `Keep the description under ${PROFILE_LIMITS.description} characters.`;
  if (draft.notes.length > PROFILE_LIMITS.notes) return `Keep the notes under ${PROFILE_LIMITS.notes} characters.`;
  if (others.some((p) => p.id !== draft.id && p.name.trim().toLowerCase() === name.toLowerCase())) {
    return "Another profile already has that name.";
  }
  return "";
}

/** The list after saving `draft`: replaces the profile with its id, or adds it. */
export function withProfile(profiles: readonly ModelProfile[], draft: ModelProfile): ModelProfile[] {
  const trimmed = { ...draft, name: draft.name.trim(), description: draft.description.trim(), notes: draft.notes.trim() };
  if (draft.id && profiles.some((p) => p.id === draft.id)) {
    return profiles.map((p) => (p.id === draft.id ? trimmed : p));
  }
  return [...profiles, trimmed];
}

/** How a profile's check reads in the list: nothing when it is fine. */
export function checkNotice(check: ModelProfileCheck | undefined): { tone: "warn" | "info"; text: string } | null {
  if (!check || check.status === "" || check.status === "ok") return null;
  switch (check.status) {
    case "unknownModel":
    case "unknownEffort":
    case "unknownVendor":
      // The profile points at something that is gone: an agent skips it.
      return { tone: "warn", text: `${check.problem} Agents skip this profile until you pick another model.` };
    case "signInRequired":
    case "notInstalled":
      return { tone: "info", text: `Not checked: ${check.vendor} is ${check.status === "signInRequired" ? "not signed in" : "not installed"}. The profile is kept.` };
    default:
      return { tone: "info", text: `Not checked: ${check.vendor}'s model list could not be read just now.` };
  }
}

export function effortLabel(value: string): string {
  return ({ xhigh: "Extra high", max: "Max" } as Record<string, string>)[value] ?? value.charAt(0).toUpperCase() + value.slice(1);
}
