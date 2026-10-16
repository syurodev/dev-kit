import { describe, expect, test } from "bun:test";
import type { ModelProfile, ModelProfileCheck } from "../../bridge/agenthub";
import { blankProfile, checkNotice, effortChoicesOf, modelChoicesOf, profileProblem, withProfile } from "./modelProfiles";

const review: ModelProfile = { id: "code-review", name: "Code review", description: "", vendor: "claude", model: "haiku", effort: "low", notes: "" };

describe("model profiles", () => {
  test("reads the model option's choices and each model's efforts", () => {
    const catalog = [{ id: "model", category: "model", options: [
      { value: "sonnet", supportedEfforts: [{ value: "low" }, { value: "high" }] },
      { value: "haiku" },
    ] }];
    const models = modelChoicesOf(catalog);
    expect(models.map((m) => m.value)).toEqual(["sonnet", "haiku"]);
    expect(effortChoicesOf(models, "sonnet").map((e) => e.value)).toEqual(["low", "high"]);
    expect(effortChoicesOf(models, "haiku")).toEqual([]);
    expect(modelChoicesOf(undefined)).toEqual([]);
  });

  test("says what is wrong with a profile in the person's words", () => {
    const draft = { ...blankProfile("claude"), name: "  ", model: "x" };
    expect(profileProblem(draft, [])).toContain("name");
    expect(profileProblem({ ...draft, name: "Docs", model: "" }, [])).toContain("model");
    expect(profileProblem({ ...draft, name: "docs", model: "m" }, [{ ...review, id: "d", name: "Docs" }])).toContain("already");
    // Editing a profile is not clashing with itself.
    expect(profileProblem({ ...review, name: "code REVIEW" }, [review])).toBe("");
    expect(profileProblem({ ...review, notes: "x".repeat(801) }, [])).toContain("notes");
  });

  test("saving replaces by id and adds otherwise, trimming what was typed", () => {
    const edited = withProfile([review], { ...review, name: " Review ", model: "sonnet" });
    expect(edited).toHaveLength(1);
    expect(edited[0]).toMatchObject({ name: "Review", model: "sonnet" });
    const added = withProfile([review], { ...blankProfile("codex"), name: "Docs", model: "gpt" });
    expect(added.map((p) => p.name)).toEqual(["Code review", "Docs"]);
    expect(added[1]?.id).toBe("");
  });

  test("a check says so only when something is wrong, and names who skips it", () => {
    const check = (status: string, problem = ""): ModelProfileCheck => ({ ...review, status, problem });
    expect(checkNotice(undefined)).toBeNull();
    expect(checkNotice(check("ok"))).toBeNull();
    const gone = checkNotice(check("unknownModel", 'claude no longer offers "haiku".'));
    expect(gone?.tone).toBe("warn");
    expect(gone?.text).toContain("Agents skip");
    expect(checkNotice(check("signInRequired"))?.text).toContain("not signed in");
    expect(checkNotice(check("notInstalled"))?.text).toContain("not installed");
    expect(checkNotice(check("unavailable"))?.tone).toBe("info");
  });
});
