import { describe, expect, it } from "bun:test";
import { filterContextFiles, mentionQueryBefore } from "./mentionQuery";

describe("mentionQueryBefore", () => {
  it("reads the query after an @ at the start or after a space", () => {
    expect(mentionQueryBefore("@")).toEqual({ query: "", start: 0 });
    expect(mentionQueryBefore("4. Dexa @sche")).toEqual({ query: "sche", start: 8 });
    expect(mentionQueryBefore("line one\n@src/app")).toEqual({ query: "src/app", start: 9 });
  });

  it("is nothing once a space ends the word, or when the @ is inside a word", () => {
    expect(mentionQueryBefore("@sche ")).toBeNull();
    expect(mentionQueryBefore("mail me at a@b.com")).toBeNull();
    expect(mentionQueryBefore("no mention here")).toBeNull();
  });
});

describe("filterContextFiles", () => {
  const files = [
    { path: "/p/schemas/user.ts", name: "user.ts", description: "schemas/user.ts" },
    { path: "/p/z/reschedule.ts", name: "reschedule.ts", description: "z/reschedule.ts" },
    { path: "/p/z/scheduler.ts", name: "scheduler.ts", description: "z/scheduler.ts" },
    { path: "/p/README.md", name: "README.md", description: "README.md" },
  ];

  it("puts a name that starts with the query first, then one that contains it, then a folder", () => {
    expect(filterContextFiles(files, "SCHE").map((file) => file.description)).toEqual([
      "z/scheduler.ts",
      "z/reschedule.ts",
      "schemas/user.ts",
    ]);
  });

  it("keeps everything for an empty query", () => {
    expect(filterContextFiles(files, "")).toHaveLength(4);
  });
});
