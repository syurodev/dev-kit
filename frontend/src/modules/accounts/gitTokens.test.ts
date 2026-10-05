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
