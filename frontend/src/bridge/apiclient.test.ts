import { describe, expect, it } from "bun:test";
import { missingVars, resolveUrl } from "./apiclient";
import { hostOf } from "./accounts";

describe("resolveUrl", () => {
  it("fills what the environment has", () => {
    expect(resolveUrl("{{baseUrl}}/users/{{id}}", { baseUrl: "https://api.github.com", id: "7" })).toBe(
      "https://api.github.com/users/7",
    );
  });

  it("leaves an unfillable placeholder in place, so the caller can tell", () => {
    // Silently dropping it would produce a URL that parses and points at the
    // wrong server, which is exactly what must not happen when the answer is
    // used to pick a credential.
    const url = resolveUrl("{{baseUrl}}/users", {});
    expect(url).toBe("{{baseUrl}}/users");
    expect(hostOf(url)).toBe("");
    expect(missingVars(url, {})).toEqual(["baseUrl"]);
  });

  it("is the pairing the account picker uses: resolve, then read the host", () => {
    const url = resolveUrl("{{baseUrl}}/repos", { baseUrl: "https://api.github.com" });
    expect(hostOf(url)).toBe("api.github.com");
  });
});
