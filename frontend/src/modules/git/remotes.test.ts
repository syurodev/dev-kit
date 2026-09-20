import { describe, expect, test } from "bun:test";
import { remoteFor } from "./remotes";

describe("remoteFor", () => {
  test("is the remote the branch tracks", () => {
    expect(remoteFor("upstream/dev", ["origin", "upstream"])).toBe("upstream");
  });

  test("prefers the longer name when one remote's name begins another's", () => {
    expect(remoteFor("team/fork/dev", ["team", "team/fork"])).toBe("team/fork");
  });

  test("falls back to the first remote when the branch tracks nothing", () => {
    expect(remoteFor("", ["origin", "upstream"])).toBe("origin");
  });

  test("is empty with no remotes", () => {
    expect(remoteFor("origin/dev", [])).toBe("");
  });
});
