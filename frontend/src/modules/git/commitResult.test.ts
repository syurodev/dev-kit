import { expect, test } from "bun:test";
import { parseCommitResult, parseHistoryResult } from "./commitResult";

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

test("parses a finished history result", () => {
  expect(parseHistoryResult({ head: commit })).toEqual({ head: commit, stopped: "", conflicted: [] });
});

test("parses a history result stopped on conflicts", () => {
  expect(parseHistoryResult({ head: commit, stopped: "rebase", conflicted: ["a.go", 3] })).toEqual({
    head: commit,
    stopped: "rebase",
    conflicted: ["a.go"],
  });
});

test("a history result needs a head or a stop", () => {
  expect(parseHistoryResult({})).toBeNull();
  expect(parseHistoryResult("nope")).toBeNull();
});
