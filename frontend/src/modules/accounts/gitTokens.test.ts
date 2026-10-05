import { describe, expect, test } from "bun:test";
import type { AccountProvider } from "@/bridge/accounts";
import { gitHostProviders, tokenNote, usernameRequired } from "./gitTokens";

const provider = (ID: string, SSHHost: string): AccountProvider => ({
  ID,
  Name: ID,
  Kinds: ["token"],
  Hosts: null,
  SSHHost,
});

describe("gitHostProviders", () => {
  test("keeps the providers that host repositories and no others", () => {
    const all = [provider("github", "github.com"), provider("anthropic", ""), provider("gitlab", "gitlab.com")];
    expect(gitHostProviders(all).map((p) => p.ID)).toEqual(["github", "gitlab"]);
  });
});

describe("usernameRequired", () => {
  test("only Bitbucket needs one", () => {
    expect(usernameRequired("bitbucket")).toBe(true);
    expect(usernameRequired("github")).toBe(false);
    expect(usernameRequired("gitlab")).toBe(false);
  });
});

describe("tokenNote", () => {
  test("says who the token is", () => {
    expect(tokenNote({ provider: "gitlab", subject: "syuro", scopes: [] })).toBe("Verified as syuro.");
  });

  test("warns when a classic GitHub token cannot reach private repositories", () => {
    const note = tokenNote({ provider: "github", subject: "acme-dev", scopes: ["read:org"] });
    expect(note).toContain("Verified as acme-dev.");
    expect(note).toContain("no “repo” scope");
  });

  test("does not warn for a token with repo, or one that lists no scopes", () => {
    expect(tokenNote({ provider: "github", subject: "acme-dev", scopes: ["repo", "read:org"] })).toBe("Verified as acme-dev.");
    expect(tokenNote({ provider: "github", subject: "acme-dev", scopes: [] })).toBe("Verified as acme-dev.");
  });

  test("a provider that cannot verify says so, and a failure is reported as one", () => {
    expect(tokenNote({ provider: "bitbucket", subject: "", scopes: [] })).toContain("cannot check a token");
    expect(tokenNote({ provider: "github", subject: "", scopes: [] }, "the provider could not be reached")).toBe(
      "Saved, but the provider could not be reached. Check the token, then verify it.",
    );
  });
});

import { canRotateToken } from "./gitTokens";

describe("tokenNote when a token was replaced", () => {
  test("says so, and keeps the rest of the wording", () => {
    expect(tokenNote({ provider: "gitlab", subject: "syuro", scopes: [] }, "", true)).toBe(
      "Token replaced. Verified as syuro.",
    );
    expect(tokenNote({ provider: "bitbucket", subject: "", scopes: [] }, "", true)).toBe(
      "Token replaced. This provider cannot check a token, so it is used as it is.",
    );
    expect(tokenNote({ provider: "github", subject: "", scopes: [] }, "the provider could not be reached", true)).toBe(
      "Token replaced, but the provider could not be reached. Check the token, then verify it.",
    );
    expect(tokenNote({ provider: "github", subject: "a", scopes: ["read:org"] }, "", true)).toContain("no “repo” scope");
  });
});

describe("canRotateToken", () => {
  const base = { kind: "token" as const, scopeOwner: "", state: "valid" as const };
  test("a shared or machine-local token or app password can", () => {
    expect(canRotateToken(base)).toBe(true);
    expect(canRotateToken({ ...base, kind: "basic" })).toBe(true);
    expect(canRotateToken({ ...base, scopeOwner: "local:abc" })).toBe(true);
  });
  test("a key, an external identity, a connection's password and a missing local secret cannot", () => {
    expect(canRotateToken({ ...base, kind: "ssh-key" })).toBe(false);
    expect(canRotateToken({ ...base, kind: "external" })).toBe(false);
    expect(canRotateToken({ ...base, kind: "secret", scopeOwner: "profile-1" })).toBe(false);
    expect(canRotateToken({ ...base, scopeOwner: "profile-1" })).toBe(false);
    expect(canRotateToken({ ...base, state: "needs_local_secret" })).toBe(false);
  });
});
