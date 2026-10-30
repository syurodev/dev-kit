import { expect, test } from "bun:test";
import { accountAnswer } from "./interaction";

// The Go hub parses exactly this shape; a drift here would turn every answer
// into a denial.
test("accountAnswer encodes the account and whether to keep the answer", () => {
  expect(accountAnswer("acct-work", true)).toBe("remember:acct-work");
  expect(accountAnswer("acct-work", false)).toBe("once:acct-work");
});

// A kind the window does not know renders as no dialog at all, so a new Go kind
// must be listed in the parser as well as drawn.
test("the group-request question is a known interaction kind", async () => {
  const { interactionKinds } = await import("./interaction");
  expect(interactionKinds).toContain("harness_group_request");
  expect(interactionKinds).toContain("harness_scope");
});
