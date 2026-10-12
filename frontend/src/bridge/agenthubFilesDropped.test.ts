import { describe, expect, it } from "bun:test";

import { parseFilesDropped } from "./agenthub";

describe("parseFilesDropped", () => {
  it("reads a drop, in either payload shape", () => {
    const record = { composer: "c1", paths: ["/a/b.txt", 3, "/c.md"], skipped: 2 };
    const expected = { composer: "c1", paths: ["/a/b.txt", "/c.md"], skipped: 2 };
    expect(parseFilesDropped(record)).toEqual(expected);
    expect(parseFilesDropped([record])).toEqual(expected);
  });

  it("is not a drop without a composer to give it to", () => {
    expect(parseFilesDropped(null)).toBeNull();
    expect(parseFilesDropped({ paths: ["/a"] })).toBeNull();
  });

  it("tolerates missing fields", () => {
    expect(parseFilesDropped({ composer: "c" })).toEqual({ composer: "c", paths: [], skipped: 0 });
  });
});
