import { describe, expect, test } from "bun:test";
import { limitMeaning, limitText, parseLimit, SUBAGENT_LIMIT_MAX } from "./subagentLimits";

describe("parseLimit", () => {
  test("blank is unset, a whole number up to the cap is a limit, zero included", () => {
    expect(parseLimit("")).toEqual({ ok: true, value: null });
    expect(parseLimit("   ")).toEqual({ ok: true, value: null });
    expect(parseLimit("0")).toEqual({ ok: true, value: 0 });
    expect(parseLimit(" 4 ")).toEqual({ ok: true, value: 4 });
    expect(parseLimit(String(SUBAGENT_LIMIT_MAX))).toEqual({ ok: true, value: SUBAGENT_LIMIT_MAX });
  });

  test("nonsense is refused in words, a value above the cap names the cap", () => {
    for (const bad of ["-1", "1.5", "two", "1e2", "+3"]) {
      const parsed = parseLimit(bad);
      expect(parsed.ok).toBe(false);
    }
    const high = parseLimit("17");
    expect(high.ok).toBe(false);
    if (!high.ok) expect(high.problem).toContain(String(SUBAGENT_LIMIT_MAX));
  });
});

describe("limitMeaning / limitText", () => {
  test("says what each kind of value does, and that nesting shares the count", () => {
    expect(limitMeaning(null)).toContain("Codex decides");
    expect(limitMeaning(0)).toContain("refused");
    expect(limitMeaning(1)).toContain("cannot start another");
    expect(limitMeaning(5)).toContain("counting any that a subagent started");
    expect(limitText(null)).toBe("");
    expect(limitText(0)).toBe("0");
  });
});
