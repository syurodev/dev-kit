import { describe, expect, test } from "bun:test";
import type { QueryClient } from "@tanstack/react-query";
import type { NoteFolder, NoteSummary } from "../../bridge/notes";
import { countNotes, invalidateNotesOnce } from "./railData";

const note = (id: string, folderId = ""): NoteSummary =>
  ({ id, title: id, folderId, tags: [], effectiveTags: [], color: "", effectiveColor: "", preview: "" }) as unknown as NoteSummary;
const folder = (id: string): NoteFolder => ({ id, name: id, color: "blue", tags: [] }) as unknown as NoteFolder;

describe("rail counts", () => {
  test("counts by folder, with notes in a missing folder as ungrouped", () => {
    const counts = countNotes([note("a", "f1"), note("b", "f1"), note("c"), note("d", "gone")], [folder("f1")]);
    expect(counts.all).toBe(4);
    expect(counts.ungrouped).toBe(2);
    expect(counts.byFolder.get("f1")).toBe(2);
  });

  // The store is identity-guarded; two loaders computing their own object for
  // the same lists would keep resetting it.
  test("the same lists give the same object", () => {
    const notes = [note("a")];
    const folders = [folder("f1")];
    expect(countNotes(notes, folders)).toBe(countNotes(notes, folders));
    expect(countNotes(notes, [folder("f1")])).not.toBe(countNotes(notes, folders));
  });
});

describe("refresh", () => {
  test("invalidates once per bump however many components watch it", () => {
    let calls = 0;
    const client = { invalidateQueries: () => { calls += 1; return Promise.resolve(); } } as unknown as QueryClient;
    invalidateNotesOnce(client, 0); // first token seen: recorded only
    invalidateNotesOnce(client, 0);
    expect(calls).toBe(0);
    invalidateNotesOnce(client, 1); // the rail
    invalidateNotesOnce(client, 1); // the dashboard, same bump
    expect(calls).toBe(1);
    invalidateNotesOnce(client, 2);
    expect(calls).toBe(2);
  });
});
