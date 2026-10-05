import { describe, expect, test } from "bun:test";
import { aliasesToBind, defaultAliasChecks, savedAsNote, suggestLabel } from "./importKeys";

describe("suggestLabel", () => {
  test("names the account after who the provider said the key is", () => {
    expect(suggestLabel("GitHub", "acme-dev", "id_work")).toBe("GitHub · acme-dev");
  });
  test("falls back to the file name, and never to nothing", () => {
    expect(suggestLabel("GitHub", "", "id_work")).toBe("GitHub · id_work");
    expect(suggestLabel("GitHub", "", "")).toBe("GitHub");
  });
});

describe("aliases", () => {
  const aliases = [
    { name: "github-work", hostName: "GitHub.com" },
    { name: "gl", hostName: "gitlab.com" },
  ];

  test("only an alias that points at the key's provider starts ticked", () => {
    const github = { Hosts: ["github.com", "api.github.com"] };
    expect(defaultAliasChecks(aliases, github)).toEqual({ "github-work": true, gl: false });
    expect(defaultAliasChecks(aliases, undefined)).toEqual({ "github-work": false, gl: false });
  });

  test("the ticked ones are what gets bound, in file order", () => {
    expect(aliasesToBind(aliases, { "github-work": true, gl: true })).toEqual(["github-work", "gl"]);
    expect(aliasesToBind(aliases, { "github-work": false, gl: true })).toEqual(["gl"]);
    expect(aliasesToBind(aliases, {})).toEqual([]);
  });
});

describe("savedAsNote", () => {
  test("lists where a key is already saved", () => {
    expect(savedAsNote({ imported: [] })).toBe("");
    expect(
      savedAsNote({
        imported: [
          { accountId: "1", provider: "github", label: "Work" },
          { accountId: "2", provider: "gitlab", label: "GL" },
        ],
      }),
    ).toBe("Work (github), GL (gitlab)");
  });
});
