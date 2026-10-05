import { expect, test } from "bun:test";
import { accountAnswer } from "./interaction";

// The Go hub parses exactly this shape; a drift here would turn every answer
// into a denial.
test("accountAnswer encodes the account and whether to keep the answer", () => {
  expect(accountAnswer("acct-work", true)).toBe("remember:acct-work");
  expect(accountAnswer("acct-work", false)).toBe("once:acct-work");
});
