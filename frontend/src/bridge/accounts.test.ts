import { describe, expect, it } from "bun:test";
import { hostOf } from "./accounts";

describe("hostOf", () => {
  it("reads the host a request authenticates to", () => {
    expect(hostOf("https://api.github.com/user/repos?page=2")).toBe("api.github.com");
    expect(hostOf("http://localhost:8080/health")).toBe("localhost");
  });

  it("has no answer while the URL still holds a variable", () => {
    // Matching an account to a template rather than to a server is how a
    // credential ends up pointed at the wrong host.
    expect(hostOf("{{base}}/user")).toBe("");
    expect(hostOf("https://{{host}}/user")).toBe("");
  });

  it("treats an unparseable or empty URL as not knowing yet", () => {
    expect(hostOf("")).toBe("");
    expect(hostOf("api.github.com/user")).toBe("");
  });
});
