import { describe, expect, test } from "bun:test";
import type { Account } from "@/bridge/accounts";
import { canRename, defaultTokenLabel, groupAccounts, linkableTokens, loginMismatch, takesLinkedToken } from "./accountGroups";

const account = (over: Partial<Account> & Pick<Account, "id" | "kind">): Account => ({
  provider: "gitlab",
  label: over.id,
  subject: "",
  scopes: [],
  env: [],
  scopeOwner: "",
  state: "valid",
  expiresAt: "",
  verifiedAt: "",
  ref: "",
  ...over,
});

describe("groupAccounts", () => {
  test("draws a linked token under its key and not again on its own", () => {
    const key = account({ id: "k", kind: "ssh-key", scopeOwner: "local:k", linkedTokenId: "t" });
    const token = account({ id: "t", kind: "token", linkedKeyIds: ["k"] });
    const other = account({ id: "o", kind: "token" });
    const rows = groupAccounts([key, token, other]);
    expect(rows.map((row) => row.account.id)).toEqual(["k", "o"]);
    expect(rows[0]?.token?.id).toBe("t");
    expect(rows[1]?.token).toBeUndefined();
  });

  test("a token serving two keys is drawn under both and never alone", () => {
    const a = account({ id: "a", kind: "ssh-key", scopeOwner: "local:a", linkedTokenId: "t" });
    const b = account({ id: "b", kind: "ssh-key", scopeOwner: "local:b", linkedTokenId: "t" });
    const token = account({ id: "t", kind: "token", linkedKeyIds: ["a", "b"] });
    const rows = groupAccounts([a, token, b]);
    expect(rows.map((row) => [row.account.id, row.token?.id])).toEqual([
      ["a", "t"],
      ["b", "t"],
    ]);
  });

  test("a link to an account that is not in the list is ignored", () => {
    const key = account({ id: "k", kind: "ssh-key", scopeOwner: "local:k", linkedTokenId: "gone" });
    expect(groupAccounts([key])).toEqual([{ account: key }]);
  });

  test("an unlinked token stays a row of its own", () => {
    const token = account({ id: "t", kind: "token" });
    expect(groupAccounts([token]).map((row) => row.account.id)).toEqual(["t"]);
  });
});

describe("linkableTokens", () => {
  const key = account({ id: "k", kind: "ssh-key", scopeOwner: "local:k" });
  test("offers the person's own tokens of the same provider that no key has", () => {
    const free = account({ id: "free", kind: "token", scopeOwner: "local:free" });
    const taken = account({ id: "taken", kind: "token", linkedKeyIds: ["z"] });
    const github = account({ id: "gh", kind: "token", provider: "github" });
    const basic = account({ id: "basic", kind: "basic" });
    const connection = account({ id: "conn", kind: "token", scopeOwner: "profile-1" });
    expect(linkableTokens(key, [free, taken, github, basic, connection, key]).map((a) => a.id)).toEqual(["free"]);
  });
});

describe("loginMismatch", () => {
  test("says nothing when the logins agree or one is unknown", () => {
    expect(loginMismatch({ subject: "Chiro" }, { subject: "chiro" })).toBe("");
    expect(loginMismatch({ subject: "" }, { subject: "chiro" })).toBe("");
    expect(loginMismatch({ subject: "chiro" }, { subject: "" })).toBe("");
  });
  test("names both logins when they differ", () => {
    expect(loginMismatch({ subject: "ann" }, { subject: "bot" })).toBe("The key is ann and the token is bot.");
  });
});

describe("what an account can do here", () => {
  test("only the person's own accounts are renamed, never an external sign-in", () => {
    expect(canRename({ kind: "token", scopeOwner: "" })).toBe(true);
    expect(canRename({ kind: "ssh-key", scopeOwner: "local:x" })).toBe(true);
    expect(canRename({ kind: "external", scopeOwner: "" })).toBe(false);
    expect(canRename({ kind: "basic", scopeOwner: "profile-1" })).toBe(false);
  });
  test("a key of a forge provider takes a token; nothing else does", () => {
    expect(takesLinkedToken({ kind: "ssh-key", scopeOwner: "local:x", provider: "gitlab" })).toBe(true);
    expect(takesLinkedToken({ kind: "ssh-key", scopeOwner: "", provider: "github" })).toBe(true);
    expect(takesLinkedToken({ kind: "ssh-key", scopeOwner: "", provider: "bitbucket" })).toBe(false);
    expect(takesLinkedToken({ kind: "token", scopeOwner: "", provider: "gitlab" })).toBe(false);
  });
  test("a token the key brings gets a name from the key", () => {
    expect(defaultTokenLabel({ label: "GitLab · id_ed25519_gitlab" })).toBe("GitLab · id_ed25519_gitlab · API token");
  });
});
