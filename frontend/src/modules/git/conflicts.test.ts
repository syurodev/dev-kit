import { describe, expect, it } from "bun:test";
import { markerCount, readConflict } from "./conflicts";
import type { Conflict } from "@/bridge/git";

function conflict(over: Partial<Conflict> = {}): Conflict {
  return {
    path: "shared.txt",
    ours: "ours\n",
    theirs: "theirs\n",
    base: "base\n",
    hasOurs: true,
    hasTheirs: true,
    hasBase: true,
    merged: "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> other\n",
    binary: false,
    ...over,
  };
}

describe("markerCount", () => {
  it("counts the three lines git writes around a conflict", () => {
    expect(markerCount(conflict().merged)).toBe(3);
  });

  it("does not count a row of equals signs underlining a heading", () => {
    // `=======` is only a marker as the WHOLE line; a setext underline under a
    // markdown heading is longer and is the document's own text.
    expect(markerCount("Title\n===========\n\nbody\n")).toBe(0);
  });

  it("is zero once the markers are edited out", () => {
    expect(markerCount("ours\ntheirs\n")).toBe(0);
  });
});

describe("readConflict", () => {
  it("offers the merged pane when both sides still have the file", () => {
    const reading = readConflict(conflict(), "main");
    expect(reading.mergeable).toBe(true);
    expect(reading.oursLabel).toBe("Ours · main");
    expect(reading.situation).toContain("Both branches changed");
  });

  it("names the branch generically when there is none", () => {
    expect(readConflict(conflict(), "").oursLabel).toBe("Ours · this branch");
  });

  it("offers only a side for a binary file", () => {
    const reading = readConflict(conflict({ binary: true, ours: "", theirs: "", merged: "" }), "main");
    expect(reading.mergeable).toBe(false);
    expect(reading.oursText).toBe("(binary)");
    expect(reading.theirsText).toBe("(binary)");
    expect(reading.situation).toContain("Binary file");
  });

  it("says which side deleted, because that is not a line merge", () => {
    const ourDeletion = readConflict(conflict({ hasOurs: false, ours: "" }), "main");
    expect(ourDeletion.mergeable).toBe(false);
    expect(ourDeletion.oursText).toBe("(deleted on this branch)");
    expect(ourDeletion.situation).toContain("This branch deleted");

    const theirDeletion = readConflict(conflict({ hasTheirs: false, theirs: "" }), "main");
    expect(theirDeletion.mergeable).toBe(false);
    expect(theirDeletion.theirsText).toBe("(deleted on the incoming branch)");
    expect(theirDeletion.situation).toContain("incoming branch deleted");
  });

  it("still merges an add/add, and says there is no common version", () => {
    const reading = readConflict(conflict({ hasBase: false, base: "" }), "main");
    expect(reading.mergeable).toBe(true);
    expect(reading.situation).toContain("no common version");
  });
});
