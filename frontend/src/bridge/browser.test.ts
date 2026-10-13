import { describe, expect, test } from "bun:test";
import { openExternalWith } from "./browser";

describe("openExternal", () => {
  test("opens http(s) and mailto", async () => {
    const opened: string[] = [];
    const open = openExternalWith({ open: async (u) => void opened.push(u), copy: async () => {}, notify: () => {} });
    await open("https://grafana.local/d/auth");
    await open("mailto:a@b.c");
    expect(opened).toEqual(["https://grafana.local/d/auth", "mailto:a@b.c"]);
  });
  test("refuses other schemes", async () => {
    const opened: string[] = [];
    const open = openExternalWith({ open: async (u) => void opened.push(u), copy: async () => {}, notify: () => {} });
    await open("javascript:alert(1)");
    await open("file:///etc/passwd");
    expect(opened).toEqual([]);
  });
  test("copies and tells when the runtime cannot open", async () => {
    const copied: string[] = [];
    const notes: string[] = [];
    const open = openExternalWith({ open: async () => { throw new Error("no runtime"); }, copy: async (u) => void copied.push(u), notify: (m) => void notes.push(m) });
    await open("https://x.test");
    expect(copied).toEqual(["https://x.test"]);
    expect(notes).toEqual(["Link copied"]);
  });
});
