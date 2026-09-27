import { describe, expect, it } from "bun:test";
import { parseListing } from "./syntax";

describe("transcript listings", () => {
  it("reads Claude's numbered Read listing as the code and its first line", () => {
    // The shape the adapter sends, from a real session's transcript.
    const read = "```\n1\tpackage com.synx.devkit.identity;\n2\t\n3\timport java.util.Optional;\n```";
    expect(parseListing(read)).toEqual({
      code: "package com.synx.devkit.identity;\n\nimport java.util.Optional;",
      startLine: 1,
      info: undefined,
    });
    expect(parseListing("```\n40\tint a;\n41\tint b;\n```")?.startLine).toBe(40);
    expect(parseListing("```\n1→first\n2→second\n```")?.code).toBe("first\nsecond");

    // Fenced but not numbered: the fence goes, the text stays.
    expect(parseListing("```console\n=== gateway ===\npackage x;\n```")).toEqual({
      code: "=== gateway ===\npackage x;",
      info: "console",
    });
    // Numbers out of sequence are content, not a listing.
    expect(parseListing("12\tone\n14\ttwo")).toBeNull();
    expect(parseListing("./deploy\n./docs")).toBeNull();
  });
});
