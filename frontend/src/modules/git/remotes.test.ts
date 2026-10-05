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

import { accountsForRemote } from "./remotes";

describe("accountsForRemote", () => {
  const accounts = [
    { id: "token", kind: "token" },
    { id: "key", kind: "ssh-key" },
    { id: "ext", kind: "external" },
  ];

  test("an SSH remote is offered keys and an HTTPS one tokens", () => {
    expect(accountsForRemote(accounts, { transport: "ssh", accountId: "" }).map((a) => a.id)).toEqual(["key"]);
    expect(accountsForRemote(accounts, { transport: "http", accountId: "" }).map((a) => a.id)).toEqual(["token"]);
  });

  test("the account already bound stays listed even when it does not fit", () => {
    expect(accountsForRemote(accounts, { transport: "ssh", accountId: "token" }).map((a) => a.id)).toEqual([
      "token",
      "key",
    ]);
  });
});
