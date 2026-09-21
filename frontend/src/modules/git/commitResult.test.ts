import { expect, test } from "bun:test";
import { parseCommitResult } from "./commitResult";

const commit = { sha: "abc123def", short: "abc123d", subject: "fix: thing" };

test("parses a commit", () => {
  expect(parseCommitResult(commit)).toEqual(commit);
});

test("returns null when sha is missing or empty", () => {
  expect(parseCommitResult({ short: "abc123d", subject: "x" })).toBeNull();
  expect(parseCommitResult({ sha: "", short: "abc123d", subject: "x" })).toBeNull();
});

test("returns null for garbage", () => {
  expect(parseCommitResult("not an object")).toBeNull();
  expect(parseCommitResult(null)).toBeNull();
});
