/**
 * The cap on Codex subagents (internal/preferences/subagentlimits.go holds the
 * same range and has the last word). Measured on Codex 0.163.0: the cap counts
 * every subagent alive at once, including ones a subagent started, and Codex's
 * own default is 3. Its depth setting is ignored by the app-server, so there is
 * no depth control here.
 */
export const SUBAGENT_LIMIT_MAX = 16;
/** What Codex 0.163.0 allows when nothing is set. Said in the UI as a fact about that version. */
export const CODEX_DEFAULT_SUBAGENTS = 3;

export type ParsedLimit = { ok: true; value: number | null } | { ok: false; problem: string };

/** What the person typed: blank is "unset"; otherwise a whole number from 0 to the cap. */
export function parseLimit(text: string): ParsedLimit {
  const trimmed = text.trim();
  if (trimmed === "") return { ok: true, value: null };
  if (!/^\d+$/.test(trimmed)) return { ok: false, problem: "Use a whole number, or leave it empty for Codex's default." };
  const value = Number(trimmed);
  if (value > SUBAGENT_LIMIT_MAX) {
    return { ok: false, problem: `At most ${SUBAGENT_LIMIT_MAX}: every subagent is a full model session.` };
  }
  return { ok: true, value };
}

export function limitText(value: number | null): string {
  return value === null ? "" : String(value);
}

/** One sentence on what a stored value means for a Codex session. */
export function limitMeaning(value: number | null): string {
  if (value === null) return `Not set: Codex decides (${CODEX_DEFAULT_SUBAGENTS} at once in version 0.163).`;
  if (value === 0) return "Codex cannot start any subagent: every request is refused.";
  if (value === 1) return "One subagent at a time, and it cannot start another.";
  return `Up to ${value} subagents at once, counting any that a subagent started.`;
}
