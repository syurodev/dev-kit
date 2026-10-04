import { describe, expect, it } from "bun:test";
import {
  formatClock,
  formatOffset,
  isSaslProtocol,
  parseBootstrap,
  parsePartition,
  prettyValue,
} from "./kafkaForm";

describe("kafka form rules", () => {
  it("only SASL protocols take a mechanism and a user", () => {
    expect(isSaslProtocol("SASL_SSL")).toBe(true);
    expect(isSaslProtocol("SASL_PLAINTEXT")).toBe(true);
    expect(isSaslProtocol("SSL")).toBe(false);
    expect(isSaslProtocol("PLAINTEXT")).toBe(false);
  });

  it("splits and trims bootstrap servers, dropping empties", () => {
    expect(parseBootstrap(" a:9092, b:9093,, ")).toEqual(["a:9092", "b:9093"]);
    expect(parseBootstrap("")).toEqual([]);
  });

  it("treats an empty partition as 'let the broker choose'", () => {
    expect(parsePartition("")).toBeUndefined();
    expect(parsePartition("  ")).toBeUndefined();
    expect(parsePartition("2")).toBe(2);
    expect(parsePartition("-1")).toBeUndefined();
    expect(parsePartition("x")).toBeUndefined();
  });

  it("formats a broker timestamp to the millisecond and survives garbage", () => {
    expect(formatClock(Date.UTC(2026, 8, 9, 9, 12, 44, 201))).toMatch(/^\d{2}:\d{2}:44\.201$/);
    expect(formatClock(undefined)).toBe("");
    expect(formatClock(Number.NaN)).toBe("");
  });

  it("pretty-prints JSON and leaves everything else alone", () => {
    expect(prettyValue('{"a":1}')).toEqual({ text: '{\n  "a": 1\n}', json: true });
    expect(prettyValue("not json")).toEqual({ text: "not json", json: false });
  });

  it("groups offset digits so a long one can be read, not counted", () => {
    expect(formatOffset(4812907)).toBe("4 812 907");
    expect(formatOffset(42)).toBe("42");
  });
});

import { validateCluster, validatePeek, validatePublish } from "./kafkaForm";

describe("Kafka input validation", () => {
  it("requires explicit environment, valid ports and SASL username", () => {
    expect(validateCluster({ name: "demo", bootstrap: "localhost:9092, [::1]:9093", env: "dev", protocol: "SSL", username: "" })).toEqual({});
    const invalid = validateCluster({ name: " ", bootstrap: "localhost:70000", env: "", protocol: "SASL_SSL", username: " " });
    expect(Object.keys(invalid)).toEqual(["name", "bootstrap", "env", "username"]);
  });
  it("allows latest offset but rejects blank/fractional/oversized limits", () => {
    expect(validatePeek({ partition: 0, offset: "", limit: "50" })).toEqual({});
    for (const limit of ["", "0", "501", "1.5"]) expect(validatePeek({ partition: 0, offset: -1, limit }).limit).toBeDefined();
    expect(validatePeek({ partition: 0, offset: -2, limit: 50 }).offset).toBeDefined();
  });
  it("does not turn a malformed publish partition into auto", () => {
    expect(validatePublish({ topic: "orders", payload: "hello", partition: "" })).toEqual({});
    expect(validatePublish({ topic: "orders", payload: "hello", partition: "x" }).partition).toBeDefined();
  });
});
