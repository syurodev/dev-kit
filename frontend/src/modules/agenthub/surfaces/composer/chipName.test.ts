import { describe, expect, test } from "bun:test";
import { splitName } from "./chipName";

describe("splitName", () => {
  test("a short name is all head", () => {
    expect(splitName("notes.md")).toEqual({ head: "notes.md", tail: "" });
  });

  test("a long name keeps its extension and the end of its stem", () => {
    expect(splitName("quarterly-financial-report-final.xlsx")).toEqual({
      head: "quarterly-financial-repo",
      tail: "rt-final.xlsx",
    });
  });

  test("the pieces always add up to the name", () => {
    const name = "SAP-Final Architecture — Galaxy G Super App Platform (consolidated from ADR-0001…0054)-191026-031020.pdf";
    const { head, tail } = splitName(name);
    expect(head + tail).toBe(name);
    expect(tail).toBe("1026-031020".slice(-8) + ".pdf");
  });

  test("a dotfile is not split at its leading dot", () => {
    const { head, tail } = splitName(".environment-production-secrets");
    expect(head + tail).toBe(".environment-production-secrets");
    expect(tail).toBe("on-secrets".slice(-8));
  });

  test("a long 'extension' is part of the name", () => {
    expect(splitName("archive.some-very-long-suffix-here")).toEqual({
      head: "archive.some-very-long-suf",
      tail: "fix-here",
    });
  });

  test("never leaves the head empty", () => {
    for (const name of ["a".repeat(20), "b".repeat(30) + ".txt", "x.".repeat(12) + "pdf"]) {
      expect(splitName(name).head.length).toBeGreaterThan(0);
    }
  });
});
