import { describe, expect, test } from "bun:test";
import { validateConnection } from "./connectionValidation";
const input = {
  name: "Local",
  host: "127.0.0.1",
  port: 5432,
  username: "postgres",
  database: "",
  password: "",
  sslMode: "disable",
  env: "dev",
};
describe("connection validation before Test or Save", () => {
  test("accepts optional database and password", () =>
    expect(validateConnection(input, ["dev"], [])).toEqual({}));
  test.each([0, -1, 65536, 3.5, NaN])("refuses invalid port %s", (port) =>
    expect(
      validateConnection({ ...input, port }, ["dev"], []).port,
    ).toBeDefined(),
  );
  test("names each missing field", () =>
    expect(
      validateConnection(
        { ...input, name: "  ", host: "", username: "", env: "" },
        ["dev"],
        [],
      ),
    ).toEqual({
      name: "Enter a connection name.",
      host: "Enter a host or server address.",
      username: "Enter a username.",
      env: "Choose an available environment.",
    }));
  test("refuses unavailable environment or SSH profile", () =>
    expect(
      validateConnection(
        { ...input, env: "removed", sshProfileId: "gone" },
        ["dev"],
        ["available"],
      ),
    ).toMatchObject({
      env: "Choose an available environment.",
      sshProfileId: "Choose an available SSH profile.",
    }));
});
