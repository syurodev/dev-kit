import { describe, expect, test } from "bun:test";
import type { CommitIdentity } from "@/bridge/git";
import { describeCommitIdentity } from "./commitIdentity";

const base: CommitIdentity = {
  name: "",
  email: "",
  source: "",
  account: "",
  accountName: "",
  accountEmail: "",
  remote: "",
};

describe("describeCommitIdentity", () => {
  test("an account's identity is said plainly and is not a warning", () => {
    const note = describeCommitIdentity({
      ...base,
      name: "Acme Dev",
      email: "dev@acme.example",
      source: "account",
      account: "github · acme-dev",
      accountName: "Acme Dev",
      accountEmail: "dev@acme.example",
    });
    expect(note.text).toBe("Committing as Acme Dev <dev@acme.example> · account “github · acme-dev”");
    expect(note.tone).toBe("normal");
  });

  test("the repository's own config winning over a different account is a warning that names both", () => {
    const note = describeCommitIdentity({
      ...base,
      name: "Repo Person",
      email: "repo@person.example",
      source: "repository",
      account: "github · acme-dev",
      accountName: "Acme Dev",
      accountEmail: "dev@acme.example",
    });
    expect(note.tone).toBe("warning");
    expect(note.hint).toContain("Acme Dev <dev@acme.example>");
  });

  test("the repository's config agreeing with the account is nothing to warn about", () => {
    const note = describeCommitIdentity({
      ...base,
      name: "Acme Dev",
      email: "dev@acme.example",
      source: "repository",
      account: "github · acme-dev",
      accountName: "Acme Dev",
      accountEmail: "dev@acme.example",
    });
    expect(note.tone).toBe("normal");
    expect(note.hint).toBe("");
  });

  test("git's identity beside an account that has none says where to set it", () => {
    const note = describeCommitIdentity({
      ...base,
      name: "Me",
      email: "me@personal.example",
      source: "git",
      account: "github · acme-dev",
    });
    expect(note.tone).toBe("warning");
    expect(note.hint).toContain("set one in Accounts");
  });

  test("git's identity with no account involved is just a statement", () => {
    const note = describeCommitIdentity({ ...base, name: "Me", email: "me@personal.example", source: "git" });
    expect(note).toEqual({
      text: "Committing as Me <me@personal.example> · git's configuration",
      hint: "",
      tone: "normal",
    });
  });

  test("no identity at all is a warning, pointing at the account when there is one", () => {
    expect(describeCommitIdentity(base).tone).toBe("warning");
    expect(describeCommitIdentity({ ...base, account: "github · acme-dev" }).hint).toContain("github · acme-dev");
  });
});
