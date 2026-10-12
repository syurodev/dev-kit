import { expect, test } from "bun:test";
import { browseIdentity, profileForBrowseKey } from "./databaseIdentity";
test("same schema/table in two database contexts never shares a buffer", () => {
  const a = browseIdentity("db-a", "public", "users");
  const b = browseIdentity("db-b", "public", "users");
  expect(a).not.toBe(b); expect(profileForBrowseKey(a, "db-b")).toBe("db-a");
  expect(browseIdentity("db-a", "a.b", "c")).not.toBe(browseIdentity("db-a", "a", "b.c"));
});
test("legacy table identities remain compatible and malformed targets fail closed", () => {
  expect(browseIdentity(undefined, "public", "users")).toBe("public.users");
  expect(profileForBrowseKey("public.users", "legacy")).toBe("legacy");
  expect(profileForBrowseKey('["db-a"]', "legacy")).toBe("");
});
