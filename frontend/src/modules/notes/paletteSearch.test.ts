import { describe, expect, it } from "bun:test";
import { buildGroups } from "./paletteSearch";
import type { NoteSummary } from "@/bridge/notes";

function note(overrides: Partial<NoteSummary> = {}): NoteSummary {
  return {
    id: "n1",
    title: "Roadmap",
    folderId: "",
    tags: [],
    effectiveTags: [],
    color: "",
    effectiveColor: "",
    preview: "Q3 goals and staffing",
    updatedAt: "2026-01-01T00:00:00Z",
    deletedAt: "",
    ...overrides,
  };
}

describe("notes buildGroups", () => {
  it("wraps whatever notes it is given in one NOTES group, in the given order", () => {
    const opened: string[] = [];
    const notes = [note({ id: "n1", title: "Roadmap" }), note({ id: "n2", title: "Standup notes" })];
    const groups = buildGroups(notes, { openNote: (n) => opened.push(n.id) });
    expect(groups).toHaveLength(1);
    expect(groups[0].heading).toBe("NOTES");
    expect(groups[0].items.map((i) => i.id)).toEqual(["note:n1", "note:n2"]);
    expect(groups[0].items[0].subtitle).toBe("Q3 goals and staffing");
  });

  it("falls back to Untitled for a note with no title", () => {
    const groups = buildGroups([note({ title: "" })], { openNote: () => undefined });
    expect(groups[0].items[0].title).toBe("Untitled");
  });

  it("omits the group entirely when there are no notes", () => {
    expect(buildGroups([], { openNote: () => undefined })).toEqual([]);
  });

  it("runs the caller's action with the picked note", () => {
    const opened: NoteSummary[] = [];
    const n = note();
    const groups = buildGroups([n], { openNote: (note) => opened.push(note) });
    groups[0].items[0].run();
    expect(opened).toEqual([n]);
  });
});
