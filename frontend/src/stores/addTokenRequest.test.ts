import { beforeEach, describe, expect, test } from "bun:test";
import { useAddTokenRequest } from "./addTokenRequest";

describe("useAddTokenRequest", () => {
  beforeEach(() => useAddTokenRequest.setState({ provider: null, keyId: "" }));

  test("is read once, with the provider and the key it was about", () => {
    useAddTokenRequest.getState().request("gitlab", "key-1");
    expect(useAddTokenRequest.getState().take()).toEqual({ provider: "gitlab", keyId: "key-1" });
    expect(useAddTokenRequest.getState().take()).toBeNull();
  });

  test("a request about no key carries none", () => {
    useAddTokenRequest.getState().request("github");
    expect(useAddTokenRequest.getState().take()).toEqual({ provider: "github", keyId: "" });
  });

  test("a later request replaces an earlier one that nobody took", () => {
    useAddTokenRequest.getState().request("gitlab", "key-1");
    useAddTokenRequest.getState().request("gitlab");
    expect(useAddTokenRequest.getState().take()).toEqual({ provider: "gitlab", keyId: "" });
  });
});
