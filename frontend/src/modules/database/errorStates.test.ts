import { describe, expect, it } from "bun:test";
import { isInsecureTLSError, isVaultLockedError } from "./errorStates";

describe("isVaultLockedError", () => {
  it("matches the spellings the bridge actually returns", () => {
    expect(isVaultLockedError("vault is locked")).toBe(true);
    expect(isVaultLockedError("Error: vault locked")).toBe(true);
    expect(isVaultLockedError("cannot read from a locked vault")).toBe(true);
  });

  // An unrelated error must not be dressed up as a vault prompt: telling
  // someone to unlock a vault that is already open sends them to a dead end.
  it("does not claim an unrelated failure is a locked vault", () => {
    expect(isVaultLockedError("connection refused")).toBe(false);
    expect(isVaultLockedError("syntax error at or near SELECT")).toBe(false);
  });
});

describe("isInsecureTLSError", () => {
  it("matches ErrInsecureTLSForRemoteHost's message", () => {
    expect(
      isInsecureTLSError("insecure TLS is only allowed for loopback database hosts"),
    ).toBe(true);
  });

  // Both halves are required. A message mentioning TLS alone is a driver
  // failure with a different fix — a certificate problem is not a refusal.
  it("does not match a plain TLS failure", () => {
    expect(isInsecureTLSError("tls: failed to verify certificate")).toBe(false);
    expect(isInsecureTLSError("no route to loopback")).toBe(false);
  });
});
